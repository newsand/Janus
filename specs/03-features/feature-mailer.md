# Feature: Mailer and outbox

## Status
Implemented in `internal/mailer`. Invite, magic link and password recovery enqueue a row in `mail_outbox` (`migrations/002_mail_outbox.sql`) in the same transaction that creates the token.

## Contract
- `POST /v1/invites`, `POST /v1/auth/magic-link` and `POST /v1/password/forgot` require a service key and `{email, subject, body}`.
- `subject` <= 200, `body` <= 10000. `body` must contain `{{link}}`, otherwise `422 {"error":"body must contain {{link}}"}`. Bind failure is `400 {"error":"invalid request"}`.
- `{{link}}` becomes `{APP_BASE_URL}{path}?token=<token>` with path `/invite`, `/magic-link` or `/password/reset`. A trailing slash on `APP_BASE_URL` is removed. Unset base URL falls back to `http://localhost`.
- Kinds: `invite`, `magic_link`, `recover`. Statuses: `pending`, `sending`, `sent`, `failed`.

## Delivery
- `MAILER_STUB=true` (default): the stub logs `[MAILER STUB]` and marks the row `sent`. No worker. `POST /v1/invites` also returns `token` and `expires_at`.
- `MAILER_STUB=false`: a worker polls every `MAILER_WORKER_INTERVAL` (default 2s), claims one row, retries with backoff until `MAILER_MAX_ATTEMPTS` (default 5), then `failed`. The invite response does not include the token.
- `MAILER_PROVIDER`: `smtp` (default; STARTTLS required) or `sendpulse` (HTTPS API). Outside `DEV_ENV`, `MAILER_STUB=false` requires the provider credentials plus `MAIL_FROM_EMAIL` and `APP_BASE_URL`.

## Acceptance
- [x] stub enqueue marks `mail_outbox` sent (`internal/mailer/mailer_test.go`)
- [x] missing `{{link}}` is rejected
- [x] smtp and sendpulse senders exist (`smtp.go`, `sendpulse.go`)
