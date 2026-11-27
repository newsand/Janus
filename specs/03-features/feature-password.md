# Feature: Password recovery

## Source
Janus code `features/password/handler.go`; `janus/specs/auth.md` §7 (old layout, removed); workspace decision #12; review-W6 TASK 4.

## Description
One-shot password recovery. `forgot` creates a recovery token; `reset` consumes it. Both are rate limited (`/v1/password/*`).

## Depends on
- feature-users (`users.FindUsableToken`), feature-mailer (delivery: NOT implemented in this checkout)

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| Forgot | command | `POST /v1/password/forgot` `{email}` (public in code, rate limited). **Always** `200 {"message":"if the email exists, a recovery link will be sent"}`: also for a missing/invalid `email`, an unparseable body (bind error), unknown email, disabled user, or token-creation failure (decision #12). For an existing enabled user: older unused recover tokens are set used; a new one-shot opaque token is created (hash only, TTL `RECOVER_TTL`, default 1h). With `MAILER_STUB=true` the raw token is logged. No email is sent in this checkout. No `subject`/`body`, no `422` | 429 rate limit exceeded |
| Reset | command | `POST /v1/password/reset` `{token, new_password (required, min 8)}` (public, rate limited). Bind error -> `400 {"error":"invalid request","details":<msg>}`; token via `FindUsableToken` (missing, used or expired -> `401 {"error":"invalid or expired token"}`); bcrypt cost 12; password hash replaced; token marked used; **all** refresh tokens of the user revoked; `200 {"message":"password reset successful"}` | 400 invalid request · 401 invalid or expired token · 500 internal error · 429 |

## Flow (given / when / then)
- Dado e-mail inválido, Quando forgot, Então 200 genérico (nunca 400).
- Dado token válido, Quando reset, Então senha trocada, token consumido, sessões revogadas.
- Dado token usado ou expirado, Quando reset, Então 401 `invalid or expired token`.

## Acceptance
- [ ] api: `features/password/handler_test.go` (SQLite em memória)
- [ ] forgot nunca distingue existe/não existe/desabilitado/inválido
