# Feature: Health

## Source
Janus code `features/health/handler.go`; `janus/specs/auth.md` §15 (old layout, removed).

## Description
Liveness plus DB ping, public, no rate limit.

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| HealthCheck | query | `GET /health` and `GET /v1/health` -> always `200 {"status":"ok","version":"alfa","database":"ok"|"error"}` (database `error` when the ping fails; the status code stays 200) | — |

## Flow
- Dado DB acessível, Quando GET /health, Então `database:"ok"`.

## Acceptance
- [ ] `docker compose up` starts app + Postgres and `/health` returns version `alfa`. No automated test exists for this package.
