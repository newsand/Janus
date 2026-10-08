# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this service is

Janus (`janus`): a JWT identity service. It only proves who a user is — it does **not** do roles/RBAC/authorization or OAuth-style redirects. Product-level authorization lives in the consuming systems (Vistoria, etc.), which map the JWT `sub` (a UUID) to their own roles.

Full product rules: `AUTH-MVP.md`. Architecture rationale/decisions: `SPEC-DRIVEN.md`. Both are canonical specs — check them before changing auth/refresh/2FA/invite/magic-link behavior, since the rules there (TTLs, rotation, reuse detection, etc.) are deliberate and "fechado" (closed/settled), not incidental.

## Commands

```bash
# Run the app locally (needs Postgres — see docker-compose.yml)
docker compose up -d
go run .

# Build
go build -o server .

# Run all tests (uses in-memory SQLite, no Postgres needed)
go test ./...

# Run tests for one feature
go test ./features/auth/...

# Run a single test
go test ./features/auth/ -run TestLogin

# Apply DB migrations manually (Postgres)
psql $DATABASE_URL -f migrations/001_initial.sql
```

There is no linter/formatter config beyond standard `go vet`/`gofmt`.

## Architecture

**Mandated structure (see `SPEC-DRIVEN.md` §3) — do not deviate:**

- Organized **by feature**, not by layer. Hexagonal/clean/DDD architecture, generic `repository`/`service` layers, and `domain`/`usecase`/`infrastructure` folders are explicitly forbidden for this project.
- `main.go` stays thin: load config, init singletons, register feature routes, start HTTP. Route registration happens in `main.go` via each feature's `RegisterRoutes(*gin.RouterGroup)`.
- `internal/config`, `internal/db`, `internal/logger` are **singletons** accessed via package-level `Get()`/`DB()` functions (`config.Get()`, `db.DB()`), not injected — this is intentional, not an oversight.
- Data access is **GORM Active Record** directly on models (`internal/models`) — no repository abstraction, no ORM-swap layer.
- GORM `BeforeCreate` hooks assign UUID v4 primary keys on every model.
- Every opaque token (refresh, invite, magic link, recover, 2FA challenge) is stored **hashed only** (`models.HashToken`, SHA-256) — never store or log the raw token.

**Feature packages** (`features/<name>/handler.go`, each with its own `RegisterRoutes`):
- `auth` — login, refresh (rotation + reuse detection), logout, magic link, `/me`
- `users` — user CRUD and invites (service-key protected)
- `password` — forgot/reset recovery flow
- `twofa` — 2FA verify/enable/disable
- `health` — liveness + DB check, mounted at both `/health` and `/v1/health`

**Cross-cutting `internal/`:**
- `internal/middleware/auth.go` — `JWTAuth()` (Bearer JWT, sets `user_id`/`jti`/`2fa_verified` in context) and `ServiceKeyAuth()` (constant-time compare against `SERVICE_KEYS`, for the CRUD/invite endpoints — there is no per-user admin role, only machine service keys)
- `internal/middleware/ratelimit.go` — in-memory rate limiting and account/IP lockout (does **not** sync across replicas — see README Security Notes)

**Auth/session model to keep consistent when touching this code:**
- Access JWT TTL 15m, refresh TTL 14d; refresh rotates on every use and belongs to a `family_id`; presenting an already-used/revoked refresh token revokes the whole family (reuse detection).
- Access JWT claims are minimal and closed: `sub`, `iat`/`exp`, `jti`, `2fa_verified`. No `roles`/`tenant` claims — adding them would violate the spec.
- If `2fa_enabled` is true, login must not issue an access token until the second factor is verified — never bypass this.
- `PATCH /users/:id` also revokes all refresh tokens when `email` changes (not for nome/telefone/cpf); there is no standalone revoke-sessions endpoint.
- Soft-disable (`disabled_at`) revokes refresh tokens immediately, but existing access JWTs remain valid until natural expiry (middleware doesn't re-check `disabled_at` per request in this MVP) — this is a known/accepted limitation, not a bug to silently fix.
- Login has a single authentication failure: `401 invalid credentials` for unknown email, no password, wrong password and disabled account alike (same body, one bcrypt each via a dummy hash). Never reintroduce an `account disabled` response; wrong password counts toward lockout, a correct password on a disabled account does not (see `AUTH-MVP.md` §7).
- Magic link and password-reset requests always respond generically regardless of whether the email exists (no email enumeration).

## Testing conventions

Tests in `features/*/handler_test.go` swap the real Postgres connection for an in-memory SQLite DB via `db.SetTestDB(...)` + `gorm.AutoMigrate` on the specific models each feature needs, then exercise handlers through a real `gin.Engine` (`httptest`). Follow this pattern for new feature tests rather than mocking at another layer.
