# Feature: Auth (login, session, magic link)

## Source
Janus code `features/auth/handler.go`, `internal/middleware`; migrated from `janus/specs/auth.md` §2-§6, §8 (old layout, removed); `janus/AUTH-MVP.md`; workspace decisions #11 (tests), login single-failure design.

## Description
Email + password login issuing an access JWT and an opaque rotating refresh token; refresh with reuse detection; logout; current-user lookup; magic link (login without password). Provisioning of accounts is in feature-users; 2FA second factor in feature-twofa.

## Depends on
- feature-users (accounts), feature-twofa (challenge verify), feature-mailer (delivery of magic link: NOT implemented in this checkout)

## Rules
**Access JWT.** HS256, signed with `JWT_SECRET`. Claims closed: `sub` (user UUID), `iat`, `exp` (= `iat` + `ACCESS_TOKEN_TTL`, default 15m), `jti` (UUID per token), `2fa_verified` (always `true`: with 2FA enabled no access token exists before the second factor succeeds). No `roles`/`tenant`/portal claims: adding them violates this spec. `JWTAuth` accepts `Authorization: Bearer <jwt>` (scheme case-insensitive), verifies signature (any HMAC method; key is `JWT_SECRET`) and expiry, and sets `user_id`, `jti`, `2fa_verified` in the Gin context. It does **not** re-read the user: a disabled user's access JWT stays valid until `exp`.

**Token response** (login, refresh, magic-link consume, 2FA verify): `{"access_token","refresh_token","token_type":"Bearer","expires_in":<ACCESS_TOKEN_TTL seconds>}`.

**Refresh token.** 32 random bytes hex, opaque (never a JWT); SHA-256 hash only in `refresh_tokens`. TTL `REFRESH_TOKEN_TTL` (default 336h = 14 days). Each login / magic-link consume / 2FA verify creates a new `family_id`; rotation keeps it. `POST /auth/refresh` revokes the presented token and issues a new access + refresh pair in the same family. Reuse: presenting an already-revoked token revokes every active token of its family and answers `401 invalid refresh token`; the revoke-on-use is an atomic conditional update, so a concurrent double use is also reuse.

**Login failure is single.** Unknown user, user without password (invite not accepted), wrong password, and disabled account all answer `401 {"error":"invalid credentials"}` (same status/body). There is no `account disabled` response (refresh is the same: `401 invalid refresh token`). Exactly one bcrypt comparison per attempt (a dummy hash of cost 12 stands in when there is no real hash) so timing does not distinguish cases. Rationale: `account disabled` would confirm the email exists.

**Lockout and rate limit** apply as in `05-non-negotiables.md`.

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| Login | command | `POST /v1/auth/login` `{email, password}`: (1) bind (`email` required+valid format, `password` required) else `400 {"error":"invalid request"}`; (2) lockout check on key `login:<email as submitted>` -> `429 {"error":"account temporarily locked"}` (same for known and unknown emails); (3) user lookup + one bcrypt; (4) unknown / no hash / wrong password (active or disabled) -> `RecordFailedAttempt` + `401 invalid credentials`; (5) correct password on a disabled account -> `401 invalid credentials`, lockout neither incremented nor cleared; (6) active + correct -> `ClearLockout`, then if `two_fa_enabled` and a secret exists -> `200 {"requires_2fa":true,"challenge_token":"<opaque>"}` (no access token; challenge creation failure -> `500 internal error`), else token response | 400 invalid request · 401 invalid credentials · 429 account temporarily locked · 429 rate limit exceeded |
| Refresh | command | `POST /v1/auth/refresh` `{refresh_token}`: hash lookup; revoked -> reuse: revoke family, `401 invalid refresh token`; expired -> `401 refresh token expired`; user missing or disabled -> `401 invalid refresh token`; atomic revoke (0 rows affected = reuse, family revoked, `401 invalid refresh token`); issue new pair, same family | 400 invalid request · 401 invalid refresh token · 401 refresh token expired · 500 internal error · 429 |
| Logout | command | `POST /v1/auth/logout` (JWT): revokes **all** active refresh tokens of the user -> `200 {"message":"logged out"}`; idempotent | 401 (JWT) · 429 |
| GetMe | query | `GET /v1/me` (JWT) -> `200 {"id","email","nome","2fa_enabled"}`; no `roles` | 401 (JWT) · 500 internal error (user not loadable) |
| RequestMagicLink | command | `POST /v1/auth/magic-link` `{email}` (public in code, rate limited): **always** `200 {"message":"if the email exists, a magic link will be sent"}` also for bind error, unknown email, disabled user, user without password, or token-creation failure. For an existing enabled user with a password: older unused magic tokens are set used; a new one-shot token is created (hash only, TTL `MAGIC_LINK_TTL`, default 15m). With `MAILER_STUB=true` the raw token is logged (`[MAILER STUB]`); the response never carries it. **Nothing is sent by email in this checkout** | 429 |
| ConsumeMagicLink | command | `POST /v1/auth/magic-link/consume` `{token}`: unknown/used/expired -> `401 invalid or expired token`; the token is marked used first, then user missing or disabled -> `401 invalid or expired token`; else `respondAuthenticated` (2FA challenge if enabled with secret, else token response, new family) | 400 invalid request · 401 invalid or expired token · 429 |

Magic link never creates an account; an invited-but-not-accepted user (no password) cannot use it. The product backend proxies the request and consume; the product frontend never calls Janus.

## Flow (given / when / then)
- Dado user ativo com senha correta, 2FA off, Quando login, Então 200 token response.
- Dado user com 2FA ligado e segredo, Quando login com senha correta, Então 200 `requires_2fa` + `challenge_token`, sem access token.
- Dado conta desabilitada com senha correta, Quando login, Então 401 `invalid credentials` e lockout inalterado.
- Dado refresh já revogado, Quando refresh, Então 401 e família inteira revogada.
- Dado e-mail inexistente, Quando magic-link, Então 200 genérico e nada gravado.

## Acceptance
- [ ] api: unit/integration via `gin.Engine` + `httptest` + SQLite em memória (`features/auth/handler_test.go`, `characterization_test.go`); falha não deixa estado parcial
- [ ] login: os quatro casos de falha indistinguíveis (status e body); senha errada em conta desabilitada conta para lockout, senha correta não
- [ ] refresh: rotação, reuso revoga a família, expirado, usuário desabilitado
- [ ] 2FA ligado nunca emite access token antes do segundo fator; 2FA desligado nunca quebra o login
