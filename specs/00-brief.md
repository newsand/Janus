# Project: Janus

Synced 2026-11-26 against the code in this checkout (`janus/`, module `github.com/newsand/janus`, Go 1.26). If code and spec disagree, that is a bug in one of them: fix it in the same change. Tasks and TDD handoffs for Janus live in the workspace spec set `teste-login/specs/` (`tasks.md`, `tdd/`, `decisions.md`); this folder is the Janus-root spec.

## Goal
JWT identity service for product frontends/backends (Vistoria, crazy, ...): it proves who a user is and issues a short-lived access JWT plus a rotating refresh token, without any OAuth/SSO redirect. Authorization stays in each product, which maps the JWT `sub` (UUID) to its own roles. It also stores and verifies per-product API keys (identity only) for third parties of a product.

Repo: https://github.com/newsand/janus (the local checkout can be behind `origin/main`; see `03-features/feature-mailer.md`).

## Roots
| root | path | role | stack (fixa) |
|---|---|---|---|
| api | . | backend | Go 1.26, Gin v1.12, GORM v1.31 (Postgres driver; SQLite driver in tests), `golang-jwt/jwt/v5`, `pquerna/otp` (TOTP), `google/uuid`, `golang.org/x/crypto/bcrypt`, Postgres 16 |

Single root, no web root: no `07-ui.md`.

## Libraries already decided
- api: Gin, GORM Active Record, `golang-jwt/jwt/v5` (HS256), `pquerna/otp/totp`, bcrypt cost 12, `google/uuid`. No logging library (own `internal/logger`). No new dependency without a spec change.
- Deploy: Nixpacks (`nixpacks.toml`, `go build -ldflags='-w -s' -o server .`, `CGO_ENABLED=0`, start `./server`) or `Dockerfile` (`golang:1.26-alpine` builder, `alpine:3.19` runtime).
- Local/test: `docker-compose.yml` = app + `postgres:16-alpine`; `go test ./...` uses in-memory SQLite (no Postgres needed).

## Architecture
Organized by feature (`features/<name>`), operations are the named handler functions (`Login`, `CreateUser`, `CreateAPIKey`, ...). Singletons for config/db/logger. Ladder at L0. See `01-architecture.md`.

## Scope
| In | Out |
|---|---|
| User database (UUID v4 ids) | Multi-portal SSO / redirects |
| Email + password login -> access JWT + refresh token | Partner hierarchy |
| Refresh with rotation and reuse detection | Unified session across apps |
| Logout (revokes all the user's refresh tokens) | Mandatory 2FA |
| Password recovery (one-shot token) | Fine-grained authorization per portal |
| Optional TOTP 2FA (`two_fa_enabled`, default off) | Open public signup |
| Rate limit + login lockout (in memory) | Roles / RBAC in this service |
| User CRUD (service key), invite, magic link | Generic email verification |
| Minimum password policy (8 chars) | "Sign out of all devices" endpoint |
| API keys for third parties of a product: create, list, revoke, rename, verify (service key) | Scopes/permissions on API keys |
| Health check | Real email delivery in this checkout (see feature-mailer) |

## Non-goals
- NO roles, tenant or portal claims in the JWT or the database.
- NO OAuth/SSO redirect; the service never redirects a browser.
- NO open signup: accounts come from CRUD, invite accept, or seed/internal API.
- NO scopes/roles on API keys; third parties never call Janus directly (the product backend does, with a service key).
- NO Redis: rate limit and lockout are per-process memory.
