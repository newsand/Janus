## Shape
- api: operations nomeadas dentro de `features/<name>/`. Proibido sem gatilho medido: hexagonal/clean/DDD, repository/service genérico, `domain`/`usecase`/`infrastructure`, DI container, `shared`/`helpers`/`utils`, microservices, camada nova.
- Singletons `config.Get()`, `db.DB()`, `logger`; GORM Active Record; sem abstração de ORM.
- Ladder: um nível por vez, registrado em 01-architecture.md.

## Security (decisions that change a security outcome; never moved to 06)
- **Identity only.** No roles/tenant/portal claims in the JWT; no `roles` column; adding them violates this spec.
- **JWT**: HS256 with `JWT_SECRET`; claims closed (`sub`, `iat`, `exp`, `jti`, `2fa_verified`); access TTL default 15m. `JWTAuth` does not re-read the user (disabled user's access JWT lives until `exp`: accepted limitation).
- **Opaque tokens** (refresh, invite, magic, recover, 2FA challenge) and **API keys**: 32 random bytes hex (API key prefixed `jns_`), SHA-256 hash only in the DB, raw never stored or logged (exception: `MAILER_STUB=true` console stub logs invite/magic/recover tokens, dev only).
- **Passwords**: bcrypt cost 12, min 8 chars wherever set (`CreateUser`, `AcceptInvite`, `Reset`); never plaintext, never serialized.
- **Refresh**: rotation on every use, family reuse detection (atomic conditional revoke), `REFRESH_TOKEN_TTL` default 336h. Logout, reset, disable and email change revoke refresh tokens.
- **Login**: one failure (`401 invalid credentials`) for unknown, no-password, wrong password, disabled; exactly one bcrypt per attempt (dummy hash); lockout checked first; wrong password counts toward lockout (also on a disabled account); correct password on a disabled account neither counts nor clears. Never reintroduce `account disabled`.
- **Lockout** (in memory, per process): key = email exactly as submitted (`login:<email>`, not IP); `LOCKOUT_THRESHOLD` (default 5) failures since the last success lock the key for `LOCKOUT_DURATION` (default 15m); success clears; while locked `429 account temporarily locked`, identical for known and unknown emails. Failures below the threshold have no time decay.
- **Rate limit** (in memory, per process): fixed window per `client IP:request path`; `RATE_LIMIT_REQUESTS` (default 5) per `RATE_LIMIT_WINDOW` (default 1m); rejected requests still count; over limit `429 rate limit exceeded`. Applies to everything under `/v1/auth/*` (login, refresh, logout, magic-link, magic-link/consume, 2fa/verify), `/v1/password/*`, and `POST /v1/invites/accept`. Does **not** apply to `/v1/users`, `POST /v1/invites`, `/v1/api-keys/*`, `/v1/me/*`, health. Not synchronized across replicas; reset on restart.
- **Service keys**: header `Authorization: Bearer <key>`; `SERVICE_KEYS` comma-separated, whitespace trimmed, empty entries dropped; constant-time comparison (SHA-256 of both sides + `ConstantTimeCompare`); missing/malformed header -> `401`, wrong -> `401 invalid service key` + warning with client IP. Protects all of `/v1/users`, all of `/v1/api-keys`, `POST /v1/invites`. It is a machine secret: no per-user admin role, CRUD never opens with a user JWT, never reaches a browser. Rotation: keep two keys (current + previous), then drop the old; two are a recommendation, validation only requires non-empty.
- **Generic responses** (no email enumeration): after a valid `{email, subject, body}`, `POST /auth/magic-link` and `POST /password/forgot` answer `200` generic for unknown, disabled and (magic link) no-password. Bind failure is `400`; missing `{{link}}` is `422`. Both routes require a service key. `POST /password/reset` and consume answer one identical `401` for unknown/used/expired.
- **API keys**: verify answers one indistinguishable `401 invalid api key`; identity only; `/v1/api-keys/*` never opens without a service key; non-UUID `owner_user_id` -> `400 invalid owner` before any query; edit is rename only (decision #2).
- **Startup secret validation** (`Config.Validate`, called only from `main`): `DEV_ENV=true` -> nothing enforced (warning logged). `DEV_ENV` unset/empty/`false` (default = production) -> the service does not start if `JWT_SECRET` has < 45 chars, `SERVICE_KEYS` is empty, or any key has < 45 chars (`MinSecretLength = 45`). The built-in default `change-me-in-production` works only in dev.
- Never log password, token, API key, TOTP secret, or service key; never read or write `.env` (only `.env.example`).
- No new dependency unless listed in 00-brief.md.

## Configuration (internal/config; `.env.example` mirrors it)
| variable | default | notes |
|---|---|---|
| `PORT` | `8080` | |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/janus?sslmode=disable` | |
| `LOG_LEVEL` | `info` | debug/info/warn(ing)/error; non-`debug` puts Gin in release mode |
| `DEV_ENV` | `false` | `true` = no secret checks, local only |
| `JWT_SECRET` | `change-me-in-production` | >= 45 chars unless `DEV_ENV=true` |
| `SERVICE_KEYS` | empty | comma-separated; each >= 45 chars unless `DEV_ENV=true`; non-empty required |
| `ACCESS_TOKEN_TTL` | `15m` | |
| `REFRESH_TOKEN_TTL` | `336h` | 14 days |
| `MAGIC_LINK_TTL` | `15m` | |
| `INVITE_TTL` | `168h` | 7 days |
| `RECOVER_TTL` | `1h` | |
| `RATE_LIMIT_REQUESTS` | `5` | |
| `RATE_LIMIT_WINDOW` | `1m` | |
| `LOCKOUT_THRESHOLD` | `5` | |
| `LOCKOUT_DURATION` | `15m` | |
| `MAILER_STUB` | `true` | `true` logs and marks the outbox row sent; `false` delivers via `MAILER_PROVIDER` |
| `MAILER_PROVIDER` | `smtp` | `smtp` or `sendpulse` when `MAILER_STUB=false` |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS` | port `587` | required for `smtp` when not stubbed (`SMTP_PORT` default `587`) |
| `SENDPULSE_USER_ID`, `SENDPULSE_SECRET` | empty | required for `sendpulse` when not stubbed |
| `MAIL_FROM_NAME` | `Janus` | |
| `MAIL_FROM_EMAIL`, `APP_BASE_URL` | empty | required when `MAILER_STUB=false` |
| `MAILER_WORKER_INTERVAL` | `2s` | real delivery only |
| `MAILER_MAX_ATTEMPTS` | `5` | |
Unparseable values silently fall back to the default. Any new env var read in code must be added here and to `.env.example`.

## Quality gate (skill quality-gate-lefthook; applies to root `janus` only: decision #3)
- Thresholds (`quality-baseline.json`): `cyclomatic_max: 10` · `cognitive_max: 15` · `duplication_max_percent: 3` · `security_fail_severity: medium` · `semgrep_configs: ["p/default"]`.
- Stages: format -> static -> security -> deadcode -> complexity -> duplication -> tests. Gitleaks always. Runner: `scripts/quality/run.sh` (+ `aggregate.sh`), report in `.quality/report.json`; check-only.
- **Tests stage** (`test_commands.go`, decision #8): `go test -race -coverpkg=./... -coverprofile=.quality/cover.out` over every package that has tests (filtered with `go list`, needed by covdata), then total of `go tool cover -func` must be **>= 50% of the whole module**. Measured 81.9%-86% (review-W9: 81.9%). Pre-existing code counts; no per-package floor.
- **Duplication** (decision #7): <= 3% for the whole project including pre-existing code, no baseline; reached by refactor (T35-T37), never by `ignore` in `.jscpd.json`. `.jscpd.json`: `minLines 5`, `minTokens 50`, ignores only generated/vendor dirs and test files (`*_test.go`, `*.test.ts`, `*.spec.ts`). Currently 0%; dead code clean.
- **Complexity** above threshold = warning + report, never a failure (17 warnings in review-W9).
- **Semgrep exclusions** (exact, never broader; no `--exclude-rule`, `.semgrepignore` or `nosemgrep`):
  - decision #11: `go.jwt-go.security.jwt-none-alg.jwt-go-none-algorithm` and `generic.secrets.security.detected-generic-secret.detected-generic-secret` (`semgrep_test_exclude_rules`), dropped from the SARIF only when the file matches `*_test.go`; both still fire in production code.
  - decision #13: container findings `dockerfile.security.missing-user.missing-user`, `yaml.docker-compose.security.no-new-privileges.no-new-privileges`, `yaml.docker-compose.security.writable-filesystem-service.writable-filesystem-service` (`semgrep_container_exclude`), dropped only for the exact paths `Dockerfile` and `docker-compose.yml`, because Docker is used only for local tests; the same rules still fire elsewhere.
- **Gitleaks** (`.gitleaks.toml`): `useDefault`, allowlist for `.quality/`, `node_modules/`, `vendor/`, `.venv/`, `graphify-out/`; custom rule `janus-api-key` (regex `\b(jns_[0-9a-fA-F]{64})\b`, keyword `jns_`) detects raw API keys. Docs and tests use placeholders (`jns_<64-hex>`, `<access_token>`, ...), no allowlist for them (decision #6).
- Lefthook: pre-commit = gitleaks + formatters (`gofmt -w`, staged); pre-push = `scripts/quality/run.sh --stage all`.
- Any failure other than complexity sends the task back to Green / the wave does not close. Never silence a finding.

## Definition of Done
- Every task: Red handoff in workspace `specs/tdd/T{id}.md` -> Green -> Verify green (tests + gate); tests registered.
- `docker compose up` starts app + Postgres and `/health` returns version `alfa`; every endpoint of 04-contracts matches its handler.
- No forbidden folders or layers; features only.
- Shared path touched (`internal/middleware`, `internal/models`, `internal/config`) -> regression on all features.

## Tests
- Integration via real `gin.Engine` + `httptest` with in-memory SQLite: `db.SetTestDB(...)` + `AutoMigrate` of the models each feature needs; no mocking at another layer.
- Existing test files per package: `features/auth` (`handler_test.go`, `characterization_test.go`), `features/users` (`handler_test.go`, `get_update_characterization_test.go`), `features/password`, `features/twofa`, `features/apikeys` (`handler_test.go`, `create_test.go`, `create_owner_uuid_test.go`, `list_test.go`, `revoke_test.go`, `update_test.go`, `verify_test.go`), `internal/config` (`config_test.go`, `load_characterization_test.go`), `internal/middleware` (`auth_test.go`, `ratelimit_test.go`). No tests: `features/health`, `internal/db`, `internal/logger`, `internal/models` (covered indirectly).
- Characterization tests pin current behavior of pre-existing code (they pass at HEAD by definition) and precede every refactor of the same package; they pin the known 2FA limitations as current behavior, which does not make them correct (limitations are listed in feature-twofa and are not fixed here).
- Bug fix -> regression test first (e.g. T28 `create_owner_uuid_test.go`). Existing tests are immutable (persisted-tester); obsolete ones are reported, never edited.
- Run: `go test ./...` (all), `go test ./features/auth/ -run TestLogin` (single).

## Code
- schema-first request structs with `binding` tags; command = explicit steps, query = no side effects; external I/O outside long transactions.
- Go conventions from `.claude/rules/gorules/` (naming, modules, typed API schemas, doc comments); `gofmt` clean.
- Never edit `.env`; new/changed env read in code -> update `.env.example` and the table above.

## Known limitations (accepted, not bugs to fix silently)
- Disabling a user revokes refresh tokens immediately but already-issued access JWTs stay valid until `exp` (<= 15 min by default).
- Changing `email` through `PATCH /users/:id` revokes all refresh tokens; `nome`, `telefone`, `cpf` revoke nothing; no standalone revoke endpoint.
- Rate limit and lockout do not synchronize between replicas and reset on restart.
- 2FA: a disabled user can still obtain tokens through verify; a TOTP code can be replayed within its window; no per-challenge attempt cap. Do not enable 2FA in production yet.
- Migrations run on boot (`migrations.Up`): pending `migrations/*.sql` are applied and recorded in `schema_migrations`.
- Magic link, forgot-password and invite enqueue `mail_outbox`. `MAILER_STUB=true` only logs; `MAILER_STUB=false` sends through SMTP or SendPulse.
