Postgres is the source of truth (no Redis). SQL in `migrations/` is the schema source; GORM models in `internal/models/models.go` match it (the Go models do not declare FKs; the SQL does, with `ON DELETE CASCADE`). All primary keys are UUID v4 (GORM `BeforeCreate` assigns one when empty; SQL default `uuid_generate_v4()`). Every opaque token and every API key is stored as SHA-256 hex hash only (`models.HashToken`, 64 chars); raw tokens come from `models.GenerateOpaqueToken()` (32 random bytes, hex, 64 chars). Timestamps are `TIMESTAMP WITH TIME ZONE`.

## User (`users`, migration 001)
| field | type | required | rule |
|---|---|---|---|
| id | uuid | yes | opaque UUID v4, nothing encoded in it |
| email | varchar(255) | yes | unique (+ index); stored as submitted (no lowercasing/normalization); create validates format (`binding: email`), update does not |
| password_hash | varchar(255) | no | nullable until an invite is accepted or a password is set; bcrypt cost 12; never serialized (`json:"-"`) |
| nome | varchar(255) | yes | never blank: trimmed (`TrimSpace`) on create/update; blank on invite accept -> invite email |
| telefone | varchar(50) | no | free text |
| cpf | varchar(14) | no | when non-blank: 11 digits or `000.000.000-00`; else `400 invalid request` / details `cpf invalid` |
| two_fa_enabled | boolean | yes | default `false` |
| two_fa_secret | varchar(64) | no | TOTP secret (base32, no padding, 20 random bytes = 32 chars); never serialized |
| created_at, updated_at | timestamptz | yes | default `NOW()` / GORM auto |
| disabled_at | timestamptz | no | set = account disabled (soft delete); partial index `WHERE disabled_at IS NULL` |

**There is no `roles` column.** Products map `sub` to roles in their own database.

## RefreshToken (`refresh_tokens`)
| field | type | required | rule |
|---|---|---|---|
| id | uuid | yes | |
| user_id | uuid | yes | FK `users(id)` cascade, indexed |
| token_hash | varchar(64) | yes | unique; SHA-256 of the opaque token (32 random bytes hex) |
| family_id | uuid | yes | indexed; new per login / magic-link consume / 2FA verify; kept on rotation |
| expires_at | timestamptz | yes | now + `REFRESH_TOKEN_TTL` |
| revoked_at | timestamptz | no | set on rotation, logout, reset, disable, email change, reuse detection; partial index on active tokens |
| created_at | timestamptz | yes | |

## Invite (`invites`)
| field | type | required | rule |
|---|---|---|---|
| id | uuid | yes | |
| email | varchar(255) | yes | indexed; the accept request carries no email, it is bound server-side to this one |
| token_hash | varchar(64) | yes | unique |
| expires_at | timestamptz | yes | now + `INVITE_TTL` |
| used_at | timestamptz | no | one-shot; older unused invites of the same email are set used on a new invite |
| created_at | timestamptz | yes | |

## MagicToken (`magic_tokens`)
id, user_id (FK cascade, indexed), token_hash (unique), expires_at (now + `MAGIC_LINK_TTL`), used_at (nullable; older unused tokens of the user are set used on a new request), created_at.

## RecoverToken (`recover_tokens`)
id, user_id (FK cascade, indexed), token_hash (unique), expires_at (now + `RECOVER_TTL`), used_at (nullable; older unused tokens of the user are set used on a new request), created_at.

## TwoFAChallenge (`two_fa_challenges`)
id, user_id (FK cascade, indexed), token_hash (unique), expires_at (now + **5 minutes**, constant), created_at. One per user: creating a challenge deletes the user's previous ones; deleted on successful verify (a wrong code leaves it).

## APIKey (`api_keys`, migration 002)
| field | type | required | rule |
|---|---|---|---|
| id | uuid | yes | UUID v4 |
| owner_user_id | uuid | yes | FK `users(id)` `ON DELETE CASCADE`, indexed; must exist and be enabled at creation |
| name | varchar(100) | no | nullable label; <= 100 chars (runes) |
| audience | varchar(100) | yes | product the key belongs to (e.g. `crazy`), 1-100 chars, indexed |
| audience_type | varchar(20) | yes | default and only accepted value `text`; reserved for a future second audience verification type (TBD: which type) |
| key_hash | varchar(64) | yes | unique; `HashToken(raw key)`; `json:"-"`, never serialized |
| expires_at | timestamptz | no | `NULL` = never expires |
| revoked_at | timestamptz | no | |
| created_at | timestamptz | yes | default `NOW()` |

Projection `api_key` (create, list, revoke, rename): `{id, owner_user_id, name, audience, audience_type, expires_at, revoked_at, created_at}`; `name`, `expires_at`, `revoked_at` serialize as `null`; never `key_hash` or the raw key. Raw key format: `jns_` + 64 hex (the prefix lets products route `jns_` vs `eyJ` and lets secret scanners detect leaks; no live/test variants).

## Not persisted
- Access JWT (stateless HS256). Claims: `sub`, `iat`, `exp`, `jti`, `2fa_verified` (always `true` in issued tokens). No roles/tenant/portal claims.
- Rate-limit counters and lockout entries (in-memory maps keyed `IP:path` and `login:<email>`).
- `mail_outbox` and `schema_migrations` exist only in the upstream version (see `03-features/feature-mailer.md`); not in this checkout's migrations.
