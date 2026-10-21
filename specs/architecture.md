# Janus — Architecture Spec

**Repo:** https://github.com/newsand/janus
**Product:** JWT identity service (no redirects, no roles)
**Behaviour spec:** [`auth.md`](auth.md) (same folder) — endpoints, token rules, flows, configuration validation.

## 1. Problem

Product frontends (Vistoria, etc.) must authenticate users without an OAuth bounce. One service proves identity and issues a JWT; authorization stays in each product.

## 2. Scope

See [`auth.md`](auth.md) §1. In short: login, refresh (rotation + reuse detection), logout, recovery, magic link, invite, CRUD via service key, optional TOTP 2FA, rate limit/lockout, Postgres. No roles, no open signup, no redirects.

## 3. Code structure (mandatory, lean)

**Forbidden:** hexagonal architecture, clean architecture, DDD, generic repository/service layers, `domain`/`usecase`/`infrastructure` folders.

**Required:**

- Organized **by feature** (`features/auth`, `features/users`, `features/password`, `features/twofa`, `features/health`), not by layer. Each feature has a `handler.go` with `RegisterRoutes(*gin.RouterGroup)`.
- A thin `main.go`: load config, validate it, init singletons, run migrations, start the mailer, register routes, serve HTTP.
- **Logger singleton** (`internal/logger`) — no logging library; colored output on stdio; level from `LOG_LEVEL`; `gin.ForceConsoleColor()`.
- **DB singleton** (`internal/db`) — Postgres through GORM, one connection pool.
- **Config singleton** (`internal/config`) and **mailer singleton** (`internal/mailer`), accessed through package-level functions (`config.Get()`, `db.DB()`, `mailer.Init` / `Enqueue*`), not injected.
- **GORM Active Record** on the models (`internal/models`; the outbox model lives in `internal/mailer`); no ORM/DB-swap abstraction.
- Gin **router groups** under `/v1` (`/v1/auth`, `/v1/users`, …); `GET /health` is also mounted without prefix.
- Complexity grows only when a feature requires it.

Cross-cutting code lives in `internal/middleware` (`JWTAuth`, `ServiceKeyAuth`, `RateLimit`, lockout helpers).

## 4. Stack

- Go 1.26 (`go.mod`, `Dockerfile` builds on `golang:1.26-alpine`)
- Gin, GORM (Postgres driver), `golang-jwt/jwt/v5`, `pquerna/otp` (TOTP), `golang.org/x/crypto/bcrypt`
- Postgres (16 in `docker-compose.yml`)
- Deploy: **Nixpacks** (`nixpacks.toml`, builds `./server` with `CGO_ENABLED=0`) or the `Dockerfile`
- Local/test: `docker compose` = app + Postgres; `go test ./...` uses in-memory SQLite

## 5. Runtime

- On startup the version is logged; `GET /health` and `GET /v1/health` return liveness, the DB check and `version` (`alfa`).
- Environment: `PORT`, `DATABASE_URL`, `LOG_LEVEL`, `DEV_ENV`, `JWT_SECRET`, `SERVICE_KEYS`, token TTLs, rate limit and lockout settings, and the mailer settings (`MAILER_STUB`, `MAILER_PROVIDER`, `SMTP_*`, `SENDPULSE_*`, `MAIL_FROM_*`, `APP_BASE_URL`, `MAILER_WORKER_INTERVAL`, `MAILER_MAX_ATTEMPTS`). Defaults and the table of each variable are in `README.md`; validation rules are in [`auth.md`](auth.md) §13.
- **Secrets at startup:** with `DEV_ENV=true` nothing is enforced; with `DEV_ENV` unset/empty/`false` (the default is production) the service does not start if `JWT_SECRET` has fewer than 45 characters or `SERVICE_KEYS` is empty or has any key under 45 characters.
- Migrations: the SQL files in `migrations/` are embedded and applied on every boot (tracked in `schema_migrations`).

## 6. Repository deliverables

1. `specs/auth.md` and `specs/architecture.md` (this folder) — the canonical specs.
2. Go code following §3–§5.
3. `README.md` — deploy and usage manual, not an architecture essay.
4. `docker-compose.yml` (Postgres + app).
5. `nixpacks.toml` for staging/production.
6. `.env.example`.
7. `INTEGRATION.md` — guide for products that consume Janus.

## 7. Definition of done

- `docker compose up` starts app + Postgres; `/health` returns version `alfa`.
- Login, refresh, logout, recovery, magic link, invite and CRUD match [`auth.md`](auth.md).
- No forbidden folders or layers; features only.

## 8. Design decisions

Decisions taken after the first cut, kept here so the code does not look like it drifted from the spec. Their behaviour is specified in [`auth.md`](auth.md).

- **Invite returns its token only in stub mode.** With `MAILER_STUB=true`, `POST /v1/invites` includes `token` and `expires_at`, so a caller without a real mailer can deliver the link another way. With `MAILER_STUB=false` the token exists only in the email (hash only in the DB).
- **`GET /v1/users` is paginated** (100 per page, `?page=N` from 1, `created_at DESC, id DESC`), replacing an unspecified hard cut at the 100 newest accounts. No search by email.
- **Login has a single failure** (`401 invalid credentials`), with one bcrypt per attempt (dummy hash when none exists). `account disabled` was removed from login and refresh because it confirmed that an email exists and is disabled.
- **`nome` is trimmed and never blank**; blank on invite accept falls back to the email.
- **Changing `email` revokes all refresh tokens**, like disabling does: the email is the identity and recovery channel, so sessions opened with the old one must not survive the change. Already-issued access JWTs live until expiry (accepted limitation).
- **`DEV_ENV` and secret validation.** The default used to be a public secret (`change-me-in-production`) and compose injected `key1,key2`; a forgotten deploy would start with a forgeable JWT secret. The default is now strict production; see §5.
- **Mailer: outbox + product-owned copy.** `POST /invites`, `POST /auth/magic-link` and `POST /password/forgot` require `subject` + `body` containing `{{link}}`; `Prepare` validates and renders it, and the outbox/worker only enqueue and deliver prepared messages. Magic-link and forgot require a **service key**.
- **Mailer providers: `smtp` and `sendpulse`** behind one `sender` interface, selected by `MAILER_PROVIDER`. SendPulse exists for hosts that block outbound SMTP ports; it uses the HTTPS API.

## 9. Consuming products

Every product that uses Janus for identity (roles and data stay in the product, never here) follows [`INTEGRATION.md`](../INTEGRATION.md) and the published guide at https://newsand.github.io/LaBottega/janus/. Pattern: JWT with a shared secret; the service key lives **only** in the product backend; invite / magic-link / forgot with `subject`/`body` + `{{link}}`; consume/accept/reset are public and called from the product.
