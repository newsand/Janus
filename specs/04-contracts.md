Prefix `/v1` on every route; `GET /health` is also mounted without prefix. Content type JSON. Verified against handlers 2026-11-26. "service key" = `Authorization: Bearer <key in SERVICE_KEYS>`; "JWT" = `Authorization: Bearer <access jwt>`.

## Common errors
- JWT routes: no header -> `401 {"error":"missing authorization header"}`; not `Bearer <x>` -> `401 {"error":"invalid authorization header"}`; bad/expired token or non-HMAC alg -> `401 {"error":"invalid token"}`.
- Service-key routes: same two header errors; wrong key -> `401 {"error":"invalid service key"}` (+ warning with client IP, key never logged).
- Rate-limited routes (`/auth/*` incl. `/auth/2fa/verify`, `/password/*`, `POST /invites/accept`): over limit -> `429 {"error":"rate limit exceeded"}`.
- Unexpected DB/internal failure -> `500 {"error":"internal error"}`.
- The service never redirects a browser.

## Endpoint index
| Method | Path | Auth | Operation | Feature |
|---|---|---|---|---|
| GET | `/health`, `/v1/health` | — | HealthCheck | health |
| POST | `/auth/login` | — (rate limit, lockout) | Login | auth |
| POST | `/auth/refresh` | — (rate limit) | Refresh | auth |
| POST | `/auth/logout` | JWT (rate limit) | Logout | auth |
| GET | `/me` | JWT | GetMe | auth |
| POST | `/auth/magic-link` | service key (rate limit) | RequestMagicLink | auth |
| POST | `/auth/magic-link/consume` | — (rate limit) | ConsumeMagicLink | auth |
| POST | `/password/forgot` | service key (rate limit) | Forgot | password |
| POST | `/password/reset` | — (rate limit) | Reset | password |
| POST | `/auth/2fa/verify` | — (rate limit) | Verify | twofa |
| POST | `/me/2fa/enable`, `/me/2fa/disable` | JWT | Enable, Disable | twofa |
| POST | `/users` | service key | CreateUser | users |
| GET | `/users?page=N` | service key | ListUsers | users |
| GET | `/users/:id` | service key | GetUser | users |
| PATCH | `/users/:id` | service key | UpdateUser | users |
| POST | `/invites` | service key | CreateInvite | users |
| POST | `/invites/accept` | — (rate limit) | AcceptInvite | users |
| POST | `/api-keys` | service key | CreateAPIKey | apikeys |
| GET | `/api-keys?owner_user_id=&audience=` | service key | ListAPIKeys | apikeys |
| POST | `/api-keys/verify` | service key | VerifyAPIKey | apikeys |
| POST | `/api-keys/:id/revoke` | service key | RevokeAPIKey | apikeys |
| PATCH | `/api-keys/:id` | service key | UpdateAPIKey | apikeys |

## health
GET /health -> HealthCheck
200: { "status": "ok", "version": "alfa", "database": "ok" | "error" }

## auth
POST /auth/login -> Login
Request: { email: string(valid email), password: string }
200: { access_token, refresh_token, token_type: "Bearer", expires_in: int }  |  { requires_2fa: true, challenge_token }
400: { error: "invalid request" } · 401: { error: "invalid credentials" } · 429: { error: "account temporarily locked" } | { error: "rate limit exceeded" }
Idempotency: n/a (failed attempts counted per submitted email).

POST /auth/refresh -> Refresh
Request: { refresh_token: string }
200: token response (same family) · 400: { error: "invalid request" } · 401: { error: "invalid refresh token" } | { error: "refresh token expired" } · 500
Reuse of a revoked token revokes the whole family.

POST /auth/logout -> Logout (JWT)
200: { message: "logged out" } · 401 (JWT). Revokes all active refresh tokens of the user.

GET /me -> GetMe (JWT)
200: { id, email, nome, "2fa_enabled": bool } · 401 · 500

POST /auth/magic-link -> RequestMagicLink (service key, rate limit)
Request: { email: string, subject: string(max 200), body: string(max 10000, must contain {{link}}) }
200: { message: "if the email exists, a magic link will be sent" } for a valid body (unknown / disabled / no password / enqueue failure)
400: { error: "invalid request" } · 422: { error: "body must contain {{link}}" } · 401 · 429

POST /auth/magic-link/consume -> ConsumeMagicLink
Request: { token: string }
200: token response | { requires_2fa: true, challenge_token } · 400: { error: "invalid request" } · 401: { error: "invalid or expired token" }

## password
POST /password/forgot -> Forgot (service key, rate limit)
Request: { email: string, subject: string(max 200), body: string(max 10000, must contain {{link}}) }
200: { message: "if the email exists, a recovery link will be sent" } for a valid body (unknown / disabled / enqueue failure)
400: { error: "invalid request" } · 422: { error: "body must contain {{link}}" } · 401 · 429

POST /password/reset -> Reset
Request: { token: string, new_password: string(min 8) }
200: { message: "password reset successful" } · 400: { error: "invalid request", details: string } · 401: { error: "invalid or expired token" } · 500
Effect: all refresh tokens of the user revoked; token consumed.

## twofa
POST /auth/2fa/verify -> Verify
Request: { challenge_token: string, code: string }
200: token response (new family) · 400: { error: "invalid request" } · 401: { error: "invalid or expired challenge" } | { error: "2FA not enabled" } | { error: "invalid code" }

POST /me/2fa/enable -> Enable (JWT)
Request: {} (setup) | { code: string } (confirm)
200: { secret, message: "scan with authenticator app, then POST code to confirm" } | { message: "2FA enabled" }
400: { error: "2FA already enabled" } | { error: "setup 2FA first by calling without code" } · 401: { error: "invalid code" } · 500

POST /me/2fa/disable -> Disable (JWT)
Request: { password?: string, code?: string } (at least one, at least one must verify)
200: { message: "2FA disabled" } · 400: { error: "password or code required" } | { error: "2FA not enabled" } · 401: { error: "invalid credentials" } · 500

## users
POST /users -> CreateUser (service key)
Request: { email: string(valid), nome: string, password?: string(min 8), telefone?: string, cpf?: string(11 digits | 000.000.000-00) }
201: { id, email, nome, created_at }
400: { error: "invalid request", details } | { error: "password must be at least 8 characters" } · 409: { error: "email already exists" } · 500

GET /users?page=N -> ListUsers (service key)
200: { users: [{ id, email, nome, telefone, cpf, "2fa_enabled", created_at, updated_at, disabled_at }], page, page_size: 100, total, total_pages }
400: { error: "invalid page" }

GET /users/:id -> GetUser (service key)
200: { id, email, nome, telefone, cpf, "2fa_enabled", created_at, updated_at, disabled_at } · 404: { error: "user not found" }

PATCH /users/:id -> UpdateUser (service key)
Request: { email?, nome?, telefone?, cpf?, disabled_at?: string } (non-empty disables, "" re-enables)
200: { message: "user updated" } · 400: { error: "invalid request"[, details] } · 404: { error: "user not found" } · 409: { error: "email already exists" } · 500
Effect: disabling or email change revokes all refresh tokens.

POST /invites -> CreateInvite (service key)
Request: { email: string(valid), subject: string(max 200), body: string(max 10000, must contain {{link}}) }
201: { message: "invite sent", email } (+ token, expires_at only when MAILER_STUB=true) · 400: { error: "invalid request" } · 422: { error: "body must contain {{link}}" } · 500

POST /invites/accept -> AcceptInvite
Request: { token: string, password: string(min 8), nome?: string }
200: { message: "account activated", user_id } · 400: { error: "invalid request", details } · 401: { error: "invalid or expired invite" } · 409: { error: "account already exists" } · 500

## api-keys (service key on all)
POST /api-keys -> CreateAPIKey
Request: { owner_user_id: uuid, audience: string(1..100), audience_type?: "text", expires_in_days?: int 1..365, name?: string(<=100) }
201: { key: "jns_<64 hex>", api_key: { id, owner_user_id, name, audience, audience_type, expires_at, revoked_at, created_at } }
400: { error: "invalid request" } | { error: "invalid owner" } (non-UUID, unknown or disabled owner) | { error: "invalid audience_type" } | { error: "expires_in_days must be between 1 and 365" } · 500
Idempotency: none. The raw key appears only here.

GET /api-keys?owner_user_id=&audience= -> ListAPIKeys
200: { api_keys: [ api_key... ] } (`[]` when empty, `created_at DESC, id DESC`, includes revoked/expired) · 400: { error: "invalid request" }

POST /api-keys/verify -> VerifyAPIKey
Request: { key: string, audience: string }
200: { valid: true, key_id, owner_user_id, audience } · 400: { error: "invalid request" } · 401: { error: "invalid api key" } (every invalid reason) · 500

POST /api-keys/:id/revoke -> RevokeAPIKey
Request: optional { audience?: string }
200: { api_key } · 400: { error: "invalid request" } · 404: { error: "api key not found" } · 500
Idempotency: repeated call keeps the original revoked_at.

PATCH /api-keys/:id -> UpdateAPIKey
Request: { name?: string|null, audience?: string } (body required)
200: { api_key } · 400: { error: "only name is editable" } | { error: "invalid request" } · 404: { error: "api key not found" } · 500
