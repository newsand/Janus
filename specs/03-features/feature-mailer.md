# Feature: Mailer and outbox (NOT IMPLEMENTED in this checkout)

## Source
Content migrated from the removed `janus/specs/auth.md` §7-§9, §12, §13 and `architecture.md` §8 (upstream `origin/main` version). Workspace `specs/CHANGELOG.md` already recorded that local `janus/` (`main` @ `c73cb50`) is behind `origin/main` and lacks mailer/outbox and embedded migrations; observed again 2026-11-26: there is no `internal/mailer`, no mailer env vars beyond `MAILER_STUB`, and no `mail_outbox` migration in this checkout.

## Status
- **Implemented here:** only `MAILER_STUB` (bool, default `true`). It does not send mail: it (a) logs invite, magic-link and recovery tokens with a `[MAILER STUB]` prefix and (b) makes `POST /v1/invites` return `token` and `expires_at`. With `MAILER_STUB=false` **nothing is delivered** and the invite token is not returned (feature gap; do not run production on this checkout expecting mail).
- **Divergence from the old docs (code wins):** in this checkout `POST /v1/auth/magic-link` and `POST /v1/password/forgot` are **public** (rate limited, no service key) and take only `{email}` (no `subject`/`body`, no `{{link}}`, no `422`); `POST /v1/invites` takes only `{email}`.
- TBD: decide whether to port the upstream mailer into this checkout (new tasks) or to keep the stub. Until then everything below is the upstream design, not current behavior.

## Upstream design (reference, not current behavior)
- Email copy belongs to the product: `POST /invites`, `POST /auth/magic-link`, `POST /password/forgot` require a **service key** and `{email, subject, body}`; `subject` <= 200 chars, `body` <= 10000 chars, `text/plain`; `body` must contain `{{link}}` (else `422`).
- `Prepare` renders `{{link}}` as `{APP_BASE_URL}{path}?token=<token>` with `path` `/invite`, `/magic-link` or `/password/reset`; trailing `/` of `APP_BASE_URL` trimmed; unset -> `http://localhost`.
- Messages go into `mail_outbox` (kinds `invite`, `magic_link`, `recover`; statuses `pending`, `sending`, `sent`, `failed`). Invite creation and enqueue share one transaction.
- `MAILER_STUB=true`: "sent" by logging, no worker. `MAILER_STUB=false`: worker polls every `MAILER_WORKER_INTERVAL` (default 2s), claims one row with `FOR UPDATE SKIP LOCKED`, retries with backoff `2^attempts` s (cap 5 min) until `MAILER_MAX_ATTEMPTS` (default 5), then `failed`.
- Providers `MAILER_PROVIDER` (default `smtp`): `smtp` (STARTTLS mandatory, PLAIN auth, spaces in `SMTP_PASS` stripped) and `sendpulse` (HTTPS API, OAuth `client_credentials` with `SENDPULSE_USER_ID`/`SENDPULSE_SECRET`, token refreshed 5 min before expiry, `MAIL_FROM_EMAIL` must be a verified sender). Adding a provider = a new `sender` implementation plus one case in `newSender`.
- Production validation (`MAILER_STUB=false`): provider must be `smtp` or `sendpulse`; `smtp` needs `SMTP_HOST`, `SMTP_USER`, `SMTP_PASS`; `sendpulse` needs `SENDPULSE_USER_ID`, `SENDPULSE_SECRET`; both need `MAIL_FROM_EMAIL` and `APP_BASE_URL`.
- Upstream also applied migrations on every boot (embedded SQL, `schema_migrations`).

## Acceptance
- [ ] n/a until the mailer is ported (TBD above)
