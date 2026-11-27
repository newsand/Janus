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
| Forgot | command | `POST /v1/password/forgot` `{email, subject, body}` (service key, rate limited). Bind failure -> `400 {"error":"invalid request"}`. Body without `{{link}}` -> `422`. A valid request is `200 {"message":"if the email exists, a recovery link will be sent"}` for unknown email, disabled user, or an enqueue failure that is not a mailer validation error. For an existing enabled user: older unused recover tokens are set used; a new one-shot opaque token is created (hash only, TTL `RECOVER_TTL`, default 1h) and enqueued. `MAILER_STUB=true` logs the token; `MAILER_STUB=false` delivers it | 400 · 401 · 422 · 429 |
| Reset | command | `POST /v1/password/reset` `{token, new_password (required, min 8)}` (public, rate limited). Bind error -> `400 {"error":"invalid request","details":<msg>}`; token via `FindUsableToken` (missing, used or expired -> `401 {"error":"invalid or expired token"}`); bcrypt cost 12; password hash replaced; token marked used; **all** refresh tokens of the user revoked; `200 {"message":"password reset successful"}` | 400 invalid request · 401 invalid or expired token · 500 internal error · 429 |

## Flow (given / when / then)
- Dado e-mail inválido, Quando forgot, Então 200 genérico (nunca 400).
- Dado token válido, Quando reset, Então senha trocada, token consumido, sessões revogadas.
- Dado token usado ou expirado, Quando reset, Então 401 `invalid or expired token`.

## Acceptance
- [ ] api: `features/password/handler_test.go` (SQLite em memória)
- [ ] forgot nunca distingue existe/não existe/desabilitado/inválido
