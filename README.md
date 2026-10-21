<p align="center">
  <img src="assets/cover.jpg" alt="Janus" width="720">
</p>

## Documentation

Full integration guide: **[newsand.github.io/LaBottega/janus](https://newsand.github.io/LaBottega/janus/)** (in Portuguese).

It walks a consuming product through the whole path: integration architecture, configuration
and secrets, the auth flows (login, refresh, logout, `/me`), validating the JWT in your own
backend without a round-trip, mapping identity to your own roles, user CRUD with the service
key, invite and login magic links, password recovery, 2FA, errors/rate limit/lockout, plus an
integration checklist and a quick endpoint reference.

Janus is part of **[La Bottega Caporali de Andrade](https://github.com/newsand/LaBottega)**,
the workshop that gathers these open-source projects and hosts their documentation.

# Janus - Identity Service

JWT authentication service for identity verification. No roles, no redirect — issues JWTs to prove who users are.

**Data access:** GORM Active Record; no ORM/DB swap abstractions.

## Quick Start (Docker Compose)

```bash
# Clone and enter
git clone https://github.com/newsand/janus.git
cd janus

# Configure (optional, defaults work for local dev)
cp .env.example .env
# Edit .env: set JWT_SECRET and SERVICE_KEYS for production

# Start
docker compose up -d

# Verify
curl http://localhost:8080/health
# {"database":"ok","status":"ok","version":"alfa"}
```

## Configuration

All settings via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | `postgres://...` | Postgres connection string |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `DEV_ENV` | `false` | `true` = dev mode: no secret strength checks (local testing only). Unset/`false` = production rules below |
| `JWT_SECRET` | - | **Required in production**: min 45 chars, else the service refuses to start |
| `SERVICE_KEYS` | - | Comma-separated keys for the service-key endpoints. Keep 2 active for rotation (recommended, not enforced). In production: not empty, **each key min 45 chars**, else the service refuses to start |
| `ACCESS_TOKEN_TTL` | `15m` | JWT access token lifetime |
| `REFRESH_TOKEN_TTL` | `336h` | Refresh token lifetime (14 days) |
| `MAGIC_LINK_TTL` | `15m` | Magic link expiry |
| `INVITE_TTL` | `168h` | Invite token expiry (7 days) |
| `RECOVER_TTL` | `1h` | Password recovery token expiry |
| `RATE_LIMIT_REQUESTS` | `5` | Max requests per client IP and path within the window (`/v1/auth/*`, `/v1/password/*`, `POST /v1/invites/accept`) |
| `RATE_LIMIT_WINDOW` | `1m` | Rate-limit window |
| `LOCKOUT_THRESHOLD` | `5` | Failed logins for one email before it is locked |
| `LOCKOUT_DURATION` | `15m` | How long a locked email stays locked |
| `MAILER_STUB` | `true` | Log emails to console (set `false` for real delivery) |
| `MAILER_PROVIDER` | `smtp` | Delivery provider when `MAILER_STUB=false`: `smtp` or `sendpulse` |
| `SMTP_HOST` | - | SMTP host (required when `MAILER_STUB=false` and `MAILER_PROVIDER=smtp`) |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS) |
| `SMTP_USER` | - | SMTP username |
| `SMTP_PASS` | - | SMTP password / app password (spaces allowed) |
| `SENDPULSE_USER_ID` | - | SendPulse OAuth `client_id` (required when `MAILER_PROVIDER=sendpulse`) |
| `SENDPULSE_SECRET` | - | SendPulse OAuth `client_secret` (required when `MAILER_PROVIDER=sendpulse`) |
| `MAIL_FROM_NAME` | `Janus` | Display name on From |
| `MAIL_FROM_EMAIL` | - | From address (required when `MAILER_STUB=false`). With `sendpulse` it must be a sender verified in your SendPulse account, otherwise delivery fails with `422 Sender is not valid` |
| `APP_BASE_URL` | - | Base URL for invite / magic-link / recover links: `{APP_BASE_URL}/invite`, `/magic-link`, `/password/reset` (required when `MAILER_STUB=false`; falls back to `http://localhost` if unset) |
| `MAILER_WORKER_INTERVAL` | `2s` | Outbox poll interval (real delivery only) |
| `MAILER_MAX_ATTEMPTS` | `5` | Max delivery attempts per outbox row |

With `DEV_ENV=true`, startup validation is skipped entirely, including the mailer requirements above (a bad `MAILER_PROVIDER` falls back to `smtp` with a warning). Outside dev mode, `MAILER_STUB=false` requires: for `smtp`, `SMTP_HOST`, `SMTP_USER`, `SMTP_PASS`; for `sendpulse`, `SENDPULSE_USER_ID`, `SENDPULSE_SECRET`; for both, `MAIL_FROM_EMAIL` and `APP_BASE_URL`. `docker-compose.yml` forces `MAILER_STUB=true`.

`MAILER_PROVIDER=sendpulse` sends over SendPulse's HTTPS API (OAuth `client_credentials` + `POST /smtp/emails`) instead of raw SMTP. Use it when the host's outbound SMTP ports (25/465/587/2525) are firewalled — common on cloud VMs — since port 443 is not.

## API Endpoints

All endpoints under `/v1` prefix.

### Health

```
GET /health
GET /v1/health
```

Returns `{"status":"ok","version":"alfa","database":"ok"}`.

### Authentication

```
POST /v1/auth/login          # Email + password → JWT
POST /v1/auth/refresh        # Refresh token → new JWT pair
POST /v1/auth/logout         # Revoke refresh tokens (requires JWT)
POST /v1/auth/magic-link     # Request magic link (service key; subject+body with {{link}})
POST /v1/auth/magic-link/consume  # Consume magic link token
GET  /v1/me                  # Get current user info (requires JWT)
```

Product frontends must not call magic-link or password/forgot directly — the product backend proxies with a service key and supplies the email copy (`subject`/`body`).

### Password Recovery

```
POST /v1/password/forgot     # Request recovery email (service key; subject+body with {{link}})
POST /v1/password/reset      # Reset password with token
```

### 2FA

**Not production-ready.** Do not set `2fa_enabled=true` in production. Known gaps: disabled users can still receive tokens via the 2FA flow; TOTP codes can be replayed; no attempt limit on the challenge.

```
POST /v1/auth/2fa/verify     # Verify 2FA code during login
POST /v1/me/2fa/enable       # Enable 2FA (requires JWT)
POST /v1/me/2fa/disable      # Disable 2FA (requires JWT + reauth)
```

### User Management (Service Key Required)

```
POST   /v1/users             # Create user
GET    /v1/users?page=N      # List users (paginated, 100 per page)
GET    /v1/users/:id         # Get user
PATCH  /v1/users/:id         # Update user (disabled_at disables; changing email or disabling revokes all refresh tokens)
POST   /v1/invites           # Create invite
POST   /v1/invites/accept    # Accept invite (public, rate limited)
```

## Authentication

### JWT Bearer Token

```bash
curl -H "Authorization: Bearer <access_token>" http://localhost:8080/v1/me
```

### Service Key

For user CRUD, invites, magic-link request, and password/forgot:

```bash
curl -H "Authorization: Bearer <service_key>" http://localhost:8080/v1/users
```

Configure `SERVICE_KEYS=key1,key2` (comma-separated). Keep 2 keys active for rotation.

## Usage Examples

### Create User (Service Key)

```bash
curl -X POST http://localhost:8080/v1/users \
  -H "Authorization: Bearer your-service-key" \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","nome":"User Name","password":"securepass123"}'
```

### Login

```bash
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"securepass123"}'
```

Response:
```json
{
  "access_token": "eyJ...",
  "refresh_token": "a1b2c3...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

### Refresh Token

```bash
curl -X POST http://localhost:8080/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"a1b2c3..."}'
```

### Create Invite

The product supplies the email copy. `body` must contain `{{link}}` (Janus replaces it with the invite URL). Missing placeholder → `422`.

```bash
curl -X POST http://localhost:8080/v1/invites \
  -H "Authorization: Bearer your-service-key" \
  -H "Content-Type: application/json" \
  -d '{"email":"newuser@example.com","subject":"Convite Crazy","body":"Ola,\n\nAceite: {{link}}\n"}'
```

Response (`MAILER_STUB=true`, the default in dev/docker-compose):
```json
{"message":"invite sent","email":"newuser@example.com","token":"a1b2c3...","expires_at":"2026-09-24T16:17:21Z"}
```

`token` and `expires_at` are only present when `MAILER_STUB=true`. With `MAILER_STUB=false`, the message is queued in `mail_outbox` and delivered by the worker; the response is just `{"message":"invite sent","email":"..."}`. Never rely on the `token` field in production.

### Magic link / password forgot (service key)

Same shape: `email`, `subject`, `body` with `{{link}}`. Always returns a generic 200 body (no email enumeration).

### Accept Invite

```bash
curl -X POST http://localhost:8080/v1/invites/accept \
  -H "Content-Type: application/json" \
  -d '{"token":"abc123...","password":"securepass123","nome":"New User"}'
```

## Deployment

### Nixpacks (Railway, Render, etc.)

The repo includes `nixpacks.toml`. Deploy by connecting to your Git repo.

Required secrets:
- `DATABASE_URL` - Postgres URL
- `JWT_SECRET` - Secure secret (45+ chars unless `DEV_ENV=true`)
- `SERVICE_KEYS` - API keys for CRUD (each 45+ chars unless `DEV_ENV=true`)

### Docker

```bash
docker build -t janus .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgres://..." \
  -e JWT_SECRET="<45+ chars>" \
  -e SERVICE_KEYS="<key 45+ chars>,<previous key 45+ chars>" \
  janus
```

### Manual

```bash
# Install Go 1.26+
go build -o server .

# Start (applies pending SQL migrations from migrations/ on boot)
./server
```

## Database

Postgres required. SQL files in `migrations/` are the schema source of truth (GORM models match). On every start the process applies pending versions (tracked in `schema_migrations`), same idea as `manage.py migrate`.

Tables: `users`, `refresh_tokens`, `invites`, `magic_tokens`, `recover_tokens`, `two_fa_challenges`, `mail_outbox`.

## Security Notes

- **Secrets at startup**: with `DEV_ENV` unset/`false` (production) the service refuses to start unless `JWT_SECRET` and every `SERVICE_KEYS` entry have at least 45 characters (and `SERVICE_KEYS` is not empty). `DEV_ENV=true` skips the check and logs a warning; it is for local testing only (the bundled `docker-compose.yml` sets it). Generate with e.g. `openssl rand -base64 48`.
- **JWT Secret**: Use a 45+ character random string in production
- **Service Keys**: Rotate keys by adding new key, updating consumers, then removing old key. Constant-time comparison used.
- **Refresh Tokens**: Stored as SHA-256 hashes. Rotation on each use. Reuse detection revokes entire family (atomic check prevents TOCTOU).
- **Passwords**: bcrypt cost 12
- **Rate Limiting**: In-memory, per client IP and path, on `/v1/auth/*`, `/v1/password/*` and `POST /v1/invites/accept`. Configure via `RATE_LIMIT_*` vars. **Note:** In-memory counters do not sync across replicas. For multi-replica deployments, implement Redis/DB-backed rate limiting or use an API gateway.
- **Account Lockout**: In-memory, login only, keyed by the email as submitted. Configure via `LOCKOUT_*` vars. Same multi-replica caveat as rate limiting.
- **Login failures**: One failure for everything — unknown email, no password set, wrong password and disabled account all return `401 {"error":"invalid credentials"}` with exactly one bcrypt comparison each (dummy hash when none exists). Wrong password counts toward lockout (disabled accounts included); a correct password on a disabled account issues no token and does not touch the lockout. There is no `account disabled` response.
- **Session revocation via service key**: `PATCH /users/:id` revokes all of the user's refresh tokens immediately when `disabled_at` is set (security block) **or** when `email` changes. Other fields (`nome`, `telefone`, `cpf`) revoke nothing. There is no standalone "revoke sessions" endpoint — the block is the mechanism.
- **Soft-disable**: When a user is disabled via `PATCH /users/:id`, all refresh tokens are revoked immediately. However, existing **access JWTs remain valid until their TTL** (~15 min). Middleware does not re-check `disabled_at` on every request.

## Client Integration

See **[INTEGRATION.md](INTEGRATION.md)** for the client integration guide — covers all auth flows (login, refresh, 2FA, password recovery, invite, magic link), token storage recommendations, error reference, and anti-patterns checklist.

The published guide at **[newsand.github.io/LaBottega/janus](https://newsand.github.io/LaBottega/janus/)** covers the same ground from the integrating product's side, with end-to-end examples (in Portuguese).

## Specs

The canonical specs live in [`specs/`](specs/): [`specs/auth.md`](specs/auth.md) (behaviour, endpoints, token and flow rules) and [`specs/architecture.md`](specs/architecture.md) (code structure and design decisions).
