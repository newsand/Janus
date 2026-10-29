# Janus — Authentication Spec

**Status:** implemented; this document describes the code on `main` exactly.
**Service job:** prove who a user is and issue a JWT. No OAuth/SSO redirect.
**Not the service's job:** roles, permissions or RBAC. Those live in the consuming products, which map the JWT `sub` (a UUID) to their own roles.

If the code and this spec disagree, that is a bug in one of them: fix it in the same change.

---

## 1. Scope

| In | Out |
|---|---|
| User database | Multi-portal SSO / redirects |
| Email + password login → access JWT + refresh token | Partner hierarchy |
| Refresh with rotation and reuse detection | Unified session across apps |
| Logout (revokes refresh tokens) | Mandatory 2FA |
| Password recovery (one-shot) | Fine-grained authorization per portal |
| Optional TOTP 2FA (`2fa_enabled`, default off) | Open public signup |
| Rate limit + login lockout | Roles / RBAC in this service |
| User CRUD (service key), invite, magic link | Generic email verification |
| UUID v4 per user | "Sign out of all devices" endpoint |
| Minimum password policy | |

---

## 2. User

Table `users`:

| Column | Notes |
|---|---|
| `id` | UUID v4, opaque, generated on create (GORM `BeforeCreate`). Nothing is encoded in it. |
| `email` | unique, not null |
| `password_hash` | nullable until the invite is accepted; bcrypt cost 12; never plaintext |
| `nome` | not null, never blank (see below) |
| `telefone` | optional |
| `cpf` | optional; when non-blank must be 11 digits or `000.000.000-00` format, else `400 cpf invalid` |
| `two_fa_enabled` | bool, default `false` |
| `two_fa_secret` | nullable TOTP secret |
| `created_at`, `updated_at` | |
| `disabled_at` | nullable; set = account disabled (soft delete) |

**There is no `roles` column.** Products map `sub` to roles in their own database.

**`nome` is never blank.** `POST /users` and `PATCH /users/:id` apply `TrimSpace` before validating: empty or whitespace-only → `400`; the trimmed value is stored. On invite accept `nome` is optional; if blank after trim it becomes the invite's email.

**Passwords** must be at least 8 characters wherever one is set (`POST /users`, invite accept, password reset). Hash: bcrypt cost 12.

### Provisioning

There is **no open signup**. Accounts are created by:

1. **CRUD** — `/v1/users` (create, read, list, update; disabling is soft via `disabled_at`). Service key only.
2. **Invite** — the product backend creates an invite (service key + email copy with `{{link}}`); the recipient accepts it and sets a password.
3. **Seed / internal API** when another system provisions users (e.g. the product's bootstrap).

`POST /users` accepts an optional `password`; without one the user has no password until an invite is accepted. `email` already in use → `409 email already exists`.

---

## 3. Access JWT

HS256, signed with `JWT_SECRET`. Claims are minimal and closed:

| Claim | Notes |
|---|---|
| `sub` | user id (UUID) |
| `iat`, `exp` | `exp` = `iat` + `ACCESS_TOKEN_TTL` (default 15m) |
| `jti` | unique id per access token |
| `2fa_verified` | always `true` in issued tokens: with 2FA enabled no access token exists before the second factor succeeds |

No `roles`, `tenant` or portal claims — adding them violates this spec.

`JWTAuth` middleware accepts `Authorization: Bearer <jwt>`, verifies the HMAC signature and expiry, and sets `user_id`, `jti` and `2fa_verified` in the request context. It does **not** re-read the user, so a disabled user's access JWT stays valid until it expires (see §14).

Token response (login, refresh, magic-link consume, 2FA verify):

```json
{ "access_token": "<jwt>", "refresh_token": "<opaque>", "token_type": "Bearer", "expires_in": 900 }
```

### `GET /v1/me` (JWT)

```json
{ "id": "<uuid-v4>", "email": "user@example.com", "nome": "...", "2fa_enabled": false }
```

No `roles`. Authorization is the receiving product's responsibility.

---

## 4. Refresh tokens

| Rule | Value |
|---|---|
| Access TTL | `ACCESS_TOKEN_TTL`, default 15 minutes |
| Refresh TTL | `REFRESH_TOKEN_TTL`, default 336h (14 days) |
| Format | 32 random bytes, hex; opaque, never a JWT |
| Storage | SHA-256 hash only, in `refresh_tokens`; the raw value is never stored or logged |
| Family | each login/magic-link/2FA-verify creates a new `family_id`; rotation keeps it |
| Rotation | every `POST /auth/refresh` revokes the presented token and issues a new access + refresh pair in the same family, in **one** database transaction (§4 procedure) |
| Reuse detection | presenting an already-revoked token revokes every active token of its family, **commits that revoke**, then answers `401 invalid refresh token`. A concurrent double use loses the conditional update and takes the same path |
| Expired token | `401 refresh token expired`. Nothing is revoked |
| Unknown token or disabled user | `401 invalid refresh token`. Nothing is revoked. A missing user row is the unknown-token case |
| Logout | `POST /auth/logout` (JWT) revokes **all** active refresh tokens of the user |

**Procedure (one transaction, then the response).** Request and response bodies stay §3. Do not commit the revoke and the insert separately. Do not write `200` before commit. Do not put two clients' refreshes in the same transaction.

1. SHA-256 the presented token. Begin one transaction.
2. Load that refresh row joined to its user (`id`, `user_id`, `family_id`, `expires_at`, `revoked_at`, `users.disabled_at`) by `token_hash`. On Postgres, `FOR UPDATE` the refresh row. On SQLite (tests only) omit the lock; step 7 still treats a lost race as reuse.
3. No row → commit nothing → `401 {"error":"invalid refresh token"}`.
4. `revoked_at` already set → set `revoked_at` on every row of that `family_id` still null, commit, then `401 {"error":"invalid refresh token"}`.
5. `expires_at` in the past → commit nothing → `401 {"error":"refresh token expired"}`.
6. User `disabled_at` set → commit nothing → `401 {"error":"invalid refresh token"}`.
7. Update `revoked_at` where `id` matches and `revoked_at IS NULL`.
   - 0 rows → step 4 (the other transaction won).
   - 1 row → insert the next refresh token in this family and sign the access JWT, still inside the transaction. Commit, then `200` with the §3 body.
8. An error on the update, the insert or the signature rolls the transaction back, **including the revoke of the presented token**, and answers `500 {"error":"internal error"}`. The presented token is still valid.

`migrations/003_refresh_family_active.sql` adds `idx_refresh_tokens_family_active` on `(family_id) WHERE revoked_at IS NULL` for step 4. Logout keeps `idx_refresh_tokens_active` on `(user_id) WHERE revoked_at IS NULL`. Do not drop either.

---

## 5. Login

`POST /v1/auth/login` `{email, password}` has **one** authentication-failure result. Unknown user, user without a password (invite not accepted), wrong password and disabled account all answer `401 {"error":"invalid credentials"}` — same status, code and body. There is no `account disabled` response (refresh behaves the same way: `401 invalid refresh token`).

Evaluation order:

1. **Lockout first.** If the account key is locked → `429 {"error":"account temporarily locked"}`, identical for known and unknown emails.
2. **Exactly one bcrypt comparison always.** With no user or no hash it compares against a dummy hash of the same cost (12), so timing does not distinguish the cases.
3. Unknown user, no password, or wrong password (active **or** disabled account) → `401 invalid credentials` and `RecordFailedAttempt`.
4. Correct password on a disabled account → no token; lockout is neither incremented nor cleared; same `401 invalid credentials`.
5. Correct password on an active account → `ClearLockout`; then, if `two_fa_enabled` and a secret exists, `200 {"requires_2fa":true,"challenge_token":"<opaque>"}` (no access token); otherwise the token response of §3.

Malformed body → `400 {"error":"invalid request"}`.

Rationale: returning `account disabled` before the password confirms the email exists and is disabled; returning it after a correct password still lets an attacker confirm a guess. Disabling notices go out by email, not through the login response.

---

## 6. Rate limiting and lockout

Both are **in-memory, per process**: counters are not shared across replicas and reset on restart.

**Rate limit** — fixed window per `client IP + request path`. Applies to everything under `/v1/auth/*` (login, refresh, logout, magic-link, magic-link/consume, 2fa/verify), `/v1/password/*`, and `POST /v1/invites/accept`. It does not apply to `/v1/users`, `POST /v1/invites` or `/v1/me`. Over the limit → `429 {"error":"rate limit exceeded"}`. `RATE_LIMIT_REQUESTS` (default 5) per `RATE_LIMIT_WINDOW` (default 1m). The map is 32 shards (FNV-32a of `IP:path`). When a shard holds at least 1024 rate entries, entries whose window has already ended are deleted. A deleted ended window is the same as a window that elapsed: the next hit starts at count 1. Do not change the key, the limit or the window to buy throughput.

**Lockout** — login only, keyed by the email exactly as submitted (not by IP). `LOCKOUT_THRESHOLD` consecutive failures (default 5) lock the key for `LOCKOUT_DURATION` (default 15m); a successful login clears it. While locked the response is `429 account temporarily locked`. Counts below the threshold are never deleted for inactivity. An entry that already reached the threshold and whose lock time is in the past is removed when that key is checked, and also when that shard's lockout map holds at least 1024 entries. Do not sweep partial counts.

---

## 7. Password recovery

1. `POST /v1/password/forgot` — **service key** — `{email, subject, body}`; `body` must contain `{{link}}` (else `422`). Called by the product backend, never by the browser.
2. The response is always `200 {"message":"if the email exists, a recovery link will be sent"}` whether or not the user exists. Disabled users get the same response and no email.
3. For an existing, enabled user: all previous unused recover tokens are invalidated; a new one-shot opaque token is created (hash only in `recover_tokens`, TTL `RECOVER_TTL`, default 1h); the message is enqueued through the mailer (§12). Enqueue failures are logged and still answer the generic `200`; only template errors (`{{link}}` missing, subject/body too long) answer `422`.
4. `POST /v1/password/reset` — public — `{token, new_password}` (min 8). Valid, unused, unexpired token → password hash replaced, token consumed, **all** refresh tokens of the user revoked → `200 {"message":"password reset successful"}`.
5. Unknown, used or expired token → `401 {"error":"invalid or expired token"}`.

---

## 8. Magic link

- **Never creates an account.** Only an existing, enabled user **with a password set**; otherwise the request still answers the generic response and sends nothing (an invited-but-not-accepted user cannot use magic link until the invite is accepted).
- `POST /v1/auth/magic-link` — **service key** — `{email, subject, body}`, `{{link}}` required (`422` if missing). Always answers `200 {"message":"if the email exists, a magic link will be sent"}`. Rate limited.
- Previous unused magic tokens of the user are invalidated; the new token is one-shot, opaque, hash only in `magic_tokens`, TTL `MAGIC_LINK_TTL` (default 15m).
- `POST /v1/auth/magic-link/consume` — public — `{token}`. Valid token → marked used → if `two_fa_enabled` (with secret) answers the 2FA challenge of §5, otherwise the token response of §3. Unknown/used/expired token or disabled user → `401 invalid or expired token`.
- The product frontend never calls the Janus magic-link request directly; its backend proxies with a service key and supplies the copy. No OAuth redirect: the product opens its own URL and posts the token to its backend.

---

## 9. Invite

- `POST /v1/invites` — **service key** — `{email, subject, body}`; `{{link}}` required (`422` otherwise).
- All unused invites for the same email are invalidated first; the new one is one-shot, opaque, hash only in `invites`, TTL `INVITE_TTL` (default 168h = 7 days). Invite creation and mail enqueue share one transaction: an enqueue/DB failure → `500`.
- Response `201 {"message":"invite sent","email":"..."}`. With `MAILER_STUB=true` it additionally carries `token` and `expires_at` (dev only; with a real mailer the token exists only in the email).
- `POST /v1/invites/accept` — public, rate limited — `{token, password (min 8), nome?}`. The token is bound to the invite's email server-side, so the accept request carries no email. Invalid/used/expired → `401 invalid or expired invite`. If a user with that email already has a password → `409 account already exists`.
- Accept creates the user, or, when a user row exists without password, sets password and `nome` on it; marks the invite used; answers `200 {"message":"account activated","user_id":"<uuid>"}`. It does not log the user in; login or magic link work afterwards.
- Invite is not public signup: only whoever holds a token enters. Until accepted, password login fails (no hash) and magic link is blocked.

---

## 10. Two-factor authentication (TOTP)

| `two_fa_enabled` | Login behaviour |
|---|---|
| `false` (default) | password ok → access + refresh |
| `true` | password ok → **no** access token; `requires_2fa` + `challenge_token` → after a valid TOTP code → access + refresh |

- Challenge: opaque, hash only in `two_fa_challenges`, TTL **5 minutes**, one per user (a new one replaces the previous), deleted on successful verify.
- `POST /v1/auth/2fa/verify` (public, rate limited) `{challenge_token, code}` → token response (new family). Bad/expired challenge → `401 invalid or expired challenge`; wrong code → `401 invalid code`.
- `POST /v1/me/2fa/enable` (JWT): without `code` generates and stores a TOTP secret and returns `{secret, message}`; with `{code}` it validates against the stored secret and sets `two_fa_enabled=true`. Already enabled → `400`. Enabling only after a valid code, so a mistyped setup cannot lock the account.
- `POST /v1/me/2fa/disable` (JWT) `{password}` and/or `{code}`; at least one must verify (reauth), else `401 invalid credentials`. Clears `two_fa_enabled` and the secret.

**Known limitations — do not enable 2FA in production yet:**

1. A disabled user can still obtain tokens through the 2FA verify step.
2. A TOTP code can be replayed within its validity window.
3. There is no per-challenge attempt cap (only the per-IP rate limit of §6).

---

## 11. Service keys

- Header `Authorization: Bearer <service_key>`. Configured as `SERVICE_KEYS`, comma-separated, whitespace trimmed, empty entries dropped.
- The SHA-256 of each configured key is cached and recomputed only when that list changes. The presented bearer is hashed on every request. Every cached hash is compared with `ConstantTimeCompare` and the results are OR-ed, with no early exit. Missing/malformed header → `401`. `valid != 1` → `401 {"error":"invalid service key"}` and a warning with the client IP (the key value is never logged). A match logs at debug only.
- Protects: all of `/v1/users`, `POST /v1/invites`, `POST /v1/auth/magic-link`, `POST /v1/password/forgot`. It is a machine secret; there is no per-user admin role and CRUD never opens with a user JWT.
- **Rotation:** keep two keys (current + previous) so consumers switch without downtime, then drop the old one. Two keys are a recommendation: validation only requires the list to be non-empty.
- It must never reach a browser.

---

## 12. Mailer and outbox

- Email copy belongs to the product: `subject` ≤ 200 chars, `body` ≤ 10000 chars, `text/plain`. Janus embeds no product copy.
- `Prepare` validates and renders: `{{link}}` is required (`422` if absent or still present after render) and is replaced by `{APP_BASE_URL}{path}?token=<token>` where `path` is `/invite` (invite), `/magic-link` (magic link) or `/password/reset` (recover). A trailing `/` on `APP_BASE_URL` is trimmed; if unset the base falls back to `http://localhost`.
- Messages go into `mail_outbox` (kinds `invite`, `magic_link`, `recover`; statuses `pending`, `sending`, `sent`, `failed`).
- `MAILER_STUB=true` (default): the message is "sent" immediately by logging it; no worker runs.
- `MAILER_STUB=false`: a worker polls every `MAILER_WORKER_INTERVAL` (default 2s), claims one pending row with `FOR UPDATE SKIP LOCKED`, and sends it. On failure it retries with backoff `2^attempts` seconds (capped at 5 minutes) until `MAILER_MAX_ATTEMPTS` (default 5), then marks the row `failed` and logs the error.
- Providers (`MAILER_PROVIDER`, default `smtp`):
  - `smtp` — STARTTLS is mandatory (a server without it is an error); PLAIN auth; spaces in `SMTP_PASS` are stripped (Gmail app passwords).
  - `sendpulse` — HTTPS API instead of raw SMTP (useful where outbound SMTP ports are firewalled): OAuth `client_credentials` with `SENDPULSE_USER_ID`/`SENDPULSE_SECRET` (token cached, refreshed 5 minutes before expiry), then `POST /smtp/emails` with `text` and base64 `html`. `MAIL_FROM_EMAIL` must be a sender verified in the SendPulse account; otherwise SendPulse answers `422 Sender is not valid` and the row exhausts its attempts.
- Adding a provider = a new sender implementation plus one case in `newSender`; the worker only talks to the `sender` interface.

---

## 13. Configuration and startup validation

Settings come from environment variables (full table in `README.md`). `main` loads config, calls `Validate()`, connects to Postgres, applies migrations, enables prepared statements, starts the mailer, then serves.

**HTTP.** `http.Server`, not `gin.Run`. `ReadHeaderTimeout` 5s, `ReadTimeout` 60s, `WriteTimeout` 60s, `IdleTimeout` 120s, `MaxHeaderBytes` 1 MiB. Every body is wrapped with `MaxBytesReader` at 1 MiB before routing. A larger body fails JSON binding as `400 {"error":"invalid request"}` on endpoints that bind a body. 1 MiB stays above the largest accepted JSON (`body` ≤ 10000). Do not lower `WriteTimeout` below 60s: login runs bcrypt cost 12 inside that window. `SIGINT` / `SIGTERM` call `Shutdown` with a 10s deadline, then stop the mailer.

- **`DEV_ENV=true`** — dev mode: `Validate()` checks nothing (defaults and short secrets work; a warning is logged). **`DEV_ENV` unset/`false` is production.** Mailer settings are validated only outside dev mode.
- **Production rules:** `JWT_SECRET` ≥ 45 characters; `SERVICE_KEYS` not empty and every key ≥ 45 characters; the service does not start otherwise. No default secret is accepted (the built-in `change-me-in-production` works only in dev).
- **With `MAILER_STUB=false` (production):** `MAILER_PROVIDER` must be `smtp` or `sendpulse`; `smtp` needs `SMTP_HOST`, `SMTP_USER`, `SMTP_PASS`; `sendpulse` needs `SENDPULSE_USER_ID` and `SENDPULSE_SECRET`; both need `MAIL_FROM_EMAIL` and `APP_BASE_URL`.

---

## 14. Database

**Postgres** is the source of truth: `users`, `refresh_tokens`, `invites`, `magic_tokens`, `recover_tokens`, `two_fa_challenges`, `mail_outbox` (plus `schema_migrations`). No Redis; rate limit and lockout are in memory (§6). Tests use in-memory SQLite.

- SQL files in `migrations/` are the schema source of truth (the GORM models match); on every boot the pending ones are applied in filename order and recorded in `schema_migrations`. Prepared statements are turned on only after that, on the same pool: a migration file is more than one statement and must not be prepared.
- `refresh_tokens` partial indexes: `idx_refresh_tokens_active (user_id) WHERE revoked_at IS NULL` (logout) and `idx_refresh_tokens_family_active (family_id) WHERE revoked_at IS NULL` (reuse, migration `003`). Do not drop either.
- Every opaque token (refresh, invite, magic, recover, 2FA challenge) is stored as a SHA-256 hash only; the raw token is never stored or logged.
- All primary keys are UUID v4.

### Known limitations

- Disabling a user (`disabled_at`) revokes refresh tokens immediately, but already-issued access JWTs stay valid until `exp` (≤ 15 min by default); the middleware does not re-check the user.
- Changing a user's `email` through `PATCH /users/:id` also revokes all their refresh tokens; `nome`, `telefone` and `cpf` revoke nothing. There is no standalone revoke endpoint.
- Rate limit and lockout do not synchronize between replicas.
- 2FA caveats of §10.

---

## 15. Endpoint contract

Public unless stated. Prefix `/v1`; `GET /health` is also mounted without prefix.

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/health` | — | `{"status":"ok","version":"alfa","database":"ok\|error"}` |
| POST | `/auth/login` | — | §5 |
| POST | `/auth/refresh` | — | §4 |
| POST | `/auth/logout` | JWT | revokes all the user's refresh tokens → `{"message":"logged out"}` |
| GET | `/me` | JWT | §3 |
| POST | `/auth/magic-link` | service key | §8 |
| POST | `/auth/magic-link/consume` | — | §8 |
| POST | `/password/forgot` | service key | §7 |
| POST | `/password/reset` | — | §7 |
| POST | `/auth/2fa/verify` | — | §10 |
| POST | `/me/2fa/enable`, `/me/2fa/disable` | JWT | §10 |
| POST | `/users` | service key | `{email, nome, password?, telefone?, cpf?}` → `201 {id,email,nome,created_at}` |
| GET | `/users?page=N` | service key | 100 per page, `created_at DESC, id DESC`; `{users,page,page_size,total,total_pages}`; invalid `page` → `400`; past the last page → empty list; no search by email |
| GET | `/users/:id` | service key | `404 user not found` |
| PATCH | `/users/:id` | service key | `{email?, nome?, telefone?, cpf?, disabled_at?}`; non-empty `disabled_at` disables, `""` re-enables; disabling or changing `email` revokes all refresh tokens |
| POST | `/invites` | service key | §9 |
| POST | `/invites/accept` | — | §9, rate limited |

Protected calls without a valid credential answer `401`. The auth service never redirects the browser.

---

## 16. Invariants (acceptance criteria)

1. Identity via JWT only; no roles in this service or its tokens.
2. Refresh: TTL + one transaction per rotation (presented token revoked and next token inserted commit together, or both roll back) + reuse revokes the family and that revoke commits before the 401. The token JSON is written only after commit.
3. Recovery: one-shot, TTL, out-of-band email, all refresh tokens revoked on reset.
4. 2FA off never breaks login; 2FA on never issues an access token before the second factor.
5. Login: the four failure cases are indistinguishable (status and body); a wrong password on a disabled account counts toward lockout, a correct one does not.
6. Outside `DEV_ENV=true` the service refuses to start with `JWT_SECRET` or any service key under 45 characters.
7. Invite, magic link and forgot: service key + `subject`/`body` with `{{link}}`, product-owned copy, delivery through the outbox.
