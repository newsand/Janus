## Unit
api: operation nomeada = one exported handler function per route (`Login`, `Refresh`, `CreateUser`, `CreateAPIKey`, ...), grouped by feature. Command = writes; query = no side effects. Path -> handler -> GORM on the model.

## Level now
| root | level | next trigger |
|---|---|---|
| api | L0 | L1 when an operation file is > ~200 lines or has 3+ private helpers |

Observation (measured): `features/auth/handler.go` (397 lines) and `features/users/handler.go` (414 lines) hold all of their feature's operations in one file and already exceed the L1 trigger; `features/apikeys` already uses one file per operation. TBD: split auth and users per operation (developer decision; no task exists).

## Ladder history
| date | root | from -> to | measured trigger |
|---|---|---|---|
| — | api | — | none recorded |

## Code structure (mandatory, lean)
**Forbidden:** hexagonal architecture, clean architecture, DDD, generic repository/service layers, `domain`/`usecase`/`infrastructure` folders, `shared`/`helpers`/`utils` packages.

**Required:**
- Organized **by feature**, not by layer. Each feature package has a `handler.go` with `RegisterRoutes(*gin.RouterGroup)` (apikeys: one file per operation besides `handler.go`).
- Thin `main.go`: `config.Load()`, `logger.Init`, `cfg.Validate()` (fatal on error; warning when `DEV_ENV=true`), `db.Init`, Gin (`gin.ForceConsoleColor()`, release mode unless `LOG_LEVEL=debug`, `gin.Recovery()` + a debug request logger), register routes, serve on `PORT`, graceful log on SIGINT/SIGTERM. **`main` does not run migrations** (see Migrations).
- Singletons through package-level functions, never injected: `config.Get()` / `config.Load()`, `db.DB()` (`db.Init`, `db.SetTestDB` for tests, `db.HealthCheck`, `db.Close`), `logger` (`Init/Debug/Info/Warn/Error/Fatal`; colored stdio, no library; level from `LOG_LEVEL`).
- GORM Active Record on the models (`internal/models`); `BeforeCreate` assigns UUID v4 ids; no ORM-swap abstraction.
- Gin router groups under `/v1`; `GET /health` is also mounted without prefix.

## Tree
```
main.go                      thin bootstrap + requestLogger
features/
  auth/handler.go            Login, Refresh, Logout, GetMe, RequestMagicLink, ConsumeMagicLink
  users/handler.go           CreateUser, ListUsers, GetUser, UpdateUser, CreateInvite, AcceptInvite
  password/handler.go        Forgot, Reset
  twofa/handler.go           Verify, Enable, Disable
  apikeys/                   handler.go (RegisterRoutes) create.go list.go lookup.go revoke.go update.go verify.go
  health/handler.go          HealthCheck
internal/
  config/config.go           Config, Load, Get, Validate, MinSecretLength
  db/db.go                   Init, DB, SetTestDB, Close, HealthCheck
  logger/logger.go           leveled colored logger
  middleware/auth.go         JWTAuth, ServiceKeyAuth, Claims, bearerToken (private)
  middleware/ratelimit.go    RateLimit, CheckLockout, RecordFailedAttempt, ClearLockout
  models/models.go           User, RefreshToken, Invite, MagicToken, RecoverToken, TwoFAChallenge, APIKey, GenerateOpaqueToken, HashToken
migrations/001_initial.sql, 002_api_keys.sql
scripts/quality/run.sh, aggregate.sh   quality gate (see 05)
quality-baseline.json, .gitleaks.toml, .jscpd.json, lefthook.yml
Dockerfile, docker-compose.yml, nixpacks.toml, .env.example
```
There is no `internal/mailer` in this checkout.

## Shared code after the dedupe refactors (decision #7; tasks T35-T37)
Only named functions inside a feature or the existing `internal/*` packages; nothing was moved to `shared`/`helpers`/`utils`.
- **auth** (`features/auth/handler.go`): `issueTokens(userID)` (new family) and `issueTokensWithFamily(userID, familyID)` (private); **exported** `RespondWithNewSession(c, userID)` (issues tokens in a new family, writes `200` or `500 internal error`) and `CurrentUser(c)` (loads the user from the `user_id` set by `JWTAuth`) are used by `twofa`. `respondAuthenticated(c, user)` (private) decides 2FA challenge vs session and is shared by `Login` and `ConsumeMagicLink`. `rejectReusedRefresh(c, familyID)` revokes the family and answers 401. `dummyPasswordHash()` (bcrypt cost 12, computed once) for constant-cost login; `dummyHashCost` is a local constant that must equal `users.BcryptCost` (not imported because users' tests import auth).
- **users** (`features/users/handler.go`): **exported** `FindUsableToken(c, rawToken, dst, isSpent, invalidMsg)` loads a single-use token row by hash and answers one identical `401` for missing or spent; used by `users.AcceptInvite` and `password.Reset`. `findUserOr404`, `validCPF` private. `password` therefore imports `users`.
- **password**: `BcryptCost = 12` (also defined in `users`).
- **middleware**: `bearerToken(c)` (private) extracts the Bearer credential or aborts with 401; used by `JWTAuth` and `ServiceKeyAuth`.
- **apikeys** (`lookup.go`, package-local): `apiKeyIDParam` (non-UUID id -> 404), `mutateScopedAPIKey` (one transaction: audience-scoped lookup, mutate, reload), `changeScopedAPIKey` (id validation + mutate + 404/500 mapping), `respondAPIKeyMutationError`; used by `revoke.go` and `update.go`. `create.go`: `bindCreateAPIKeyRequest` (ordered validation, returns the 400 message), `validExpiresInDays`, `expiresAt`. `verify.go`: `findValidAPIKey`.
- Import graph: `twofa -> auth`, `password -> users`; all features -> `internal/*`.

## Boundaries
- Input only through explicit request structs with `binding` tags (`ShouldBindJSON` / `ShouldBindQuery`); handlers answer typed/structured JSON; DB errors never leak (`500 {"error":"internal error"}`).
- Janus returns identity only; roles/scopes never appear in tokens, rows or responses.
- Opaque tokens and API keys are stored as SHA-256 hash only (`models.HashToken`); raw values are never stored or logged.
- Service key = machine secret, never reaches a browser.
- Cross-cutting code lives only in `internal/middleware`.

## Consistency
- Refresh rotation: revoke-on-use is an atomic conditional `UPDATE ... WHERE id = ? AND revoked_at IS NULL`; `RowsAffected == 0` is treated as reuse (concurrent double use).
- API key create: owner lookup + insert in one transaction. Revoke/rename: scoped lookup + mutation + reload in one transaction; revoke is idempotent.
- Invites: older unused invites of the email are marked used, then the new row is created (two statements, no transaction). Magic/recover: same pattern per user.
- Other commands (login lockout, tokens) are single statements or short sequences without an explicit transaction. Rate limit and lockout are in memory: not shared across replicas, reset on restart.
- Idempotency: only revoke API key. No idempotency keys elsewhere.

## Observability
Leveled colored logger (`LOG_LEVEL`: debug/info/warn/error, default info). `requestLogger` logs `METHOD path status` at debug only. Invalid service keys log a warning with the client IP (never the key). Never log passwords, raw tokens, API keys or TOTP secrets (exception: with `MAILER_STUB=true` the stub deliberately logs invite, magic-link and recovery tokens to the console, dev only).

## Runtime
- Startup logs the version (`alfa`, hard-coded in `config.Load`).
- Environment variables (all read in `internal/config`): `PORT`, `DATABASE_URL`, `LOG_LEVEL`, `DEV_ENV`, `JWT_SECRET`, `SERVICE_KEYS`, the TTL and rate-limit variables, and the mailer set (`MAILER_STUB`, `MAILER_PROVIDER`, SMTP, SendPulse, `MAIL_FROM_*`, `APP_BASE_URL`, `MAILER_WORKER_INTERVAL`, `MAILER_MAX_ATTEMPTS`). Defaults are in `05-non-negotiables.md` and `.env.example`. An unparseable duration/int/bool silently falls back to the default.
- DB pool: max open 25, max idle 5, conn lifetime 1h, idle 30m; ping at startup (fatal on failure).
- Compose: app + `postgres:16-alpine` (host port 5433; the app applies `migrations/` on start, Postgres does not), `MAILER_STUB=true`, `DEV_ENV` default `true`, `SERVICE_KEYS` default `key1,key2`.

## Migrations
`migrations/001_initial.sql`, `migrations/002_mail_outbox.sql`, `migrations/002_api_keys.sql` and `migrations/003_refresh_family_active.sql`. On every start `migrations.Up` applies pending files and records them in `schema_migrations`. Nothing else may run these files: running one outside `migrations.Up` (a `psql -f`, or a mount at `/docker-entrypoint-initdb.d`) creates its tables without recording the version, so the next start re-runs it and fails on `CREATE TABLE`.

## Design decisions (kept so the code does not look like it drifted)
- **Invite returns its token only in stub mode.** With `MAILER_STUB=true`, `POST /v1/invites` includes `token` and `expires_at` and the stub logs the message. With `MAILER_STUB=false` the token stays in the email (`internal/mailer`).
- **`GET /v1/users` is paginated** (100 per page, `?page=N`, `created_at DESC, id DESC`); no search by email.
- **Login has a single failure** (`401 invalid credentials`), exactly one bcrypt per attempt (dummy hash when none exists). `account disabled` was removed from login and refresh.
- **`nome` is trimmed and never blank**; blank on invite accept falls back to the email.
- **Changing `email` revokes all refresh tokens**, like disabling does. Issued access JWTs live until expiry (accepted limitation).
- **`DEV_ENV` and secret validation**: default is strict production (see 05).
- **Forgot-password and magic link** require a service key and `{email, subject, body}`. Bind failure is `400`. A valid body gets the generic `200` for unknown or disabled accounts.
- **API keys**: hash-only, identity-only verify with one indistinguishable 401, rename-only edit (decision #2), non-UUID owner -> `400 invalid owner` before any query (decision #9, T28).
- **Quality gate** for Janus only (decisions #3, #7, #8, #11, #13): see 05.

## Consuming products
Every product that uses Janus for identity follows `INTEGRATION.md` and the published guide. Pattern: JWT with shared `JWT_SECRET` (product validates HS256 locally, reads `sub`); service key only in the product backend; invite/magic-link/forgot/reset/accept/consume called from the product backend; API keys: third parties call the product backend, which calls `POST /v1/api-keys/verify` with its service key.

## Repository deliverables
`specs/` (this folder), Go code per this document, `README.md` (deploy/usage), `docker-compose.yml`, `Dockerfile`, `nixpacks.toml`, `.env.example`, `INTEGRATION.md`, `quality-baseline.json`, `scripts/quality/`, `lefthook.yml`, `.gitleaks.toml`, `.jscpd.json`.
