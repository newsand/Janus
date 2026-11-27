# Feature: API keys

## Source
Janus code `features/apikeys/*.go`, `migrations/002_api_keys.sql`, `internal/models` `APIKey`; migrated from `janus/specs/auth.md` §17/§17.1 and `architecture.md` §10 (old layout, removed); workspace decisions #2 (edit = rename only, closed), #9 (non-UUID owner -> 400, task T28); workspace feature `feature-janus-api-keys.md` (tasks T01, T04-T07, T19, T28).

## Description
An API key is a credential a third-party system uses to call a **product backend**, never Janus. Janus stores and verifies keys; the product authorizes. Janus returns identity only: no scopes, roles or permissions. Service keys (05) are unchanged and remain the product <-> Janus trust. No new environment variable; no rate limit on this group.

## Depends on
- feature-users (owner must exist and be enabled)

## Rules
- Key format `jns_` + 64 hex (32 random bytes, `models.GenerateOpaqueToken`). The raw key is returned **once**, in the create response; only its SHA-256 hash is stored; never logged, never in list/revoke/rename/verify responses.
- Group `/v1/api-keys` behind `ServiceKeyAuth()` (missing/wrong key -> its `401`). Unexpected DB errors -> `500 {"error":"internal error"}`.
- `name` is the only mutable attribute besides `revoked_at`; secret, owner, audience, `audience_type` and expiry are immutable (changing expiry = revoke + create).
- `audience_type` accepts only `text`; reserved for a second verification type (TBD: which type, no schema/contract change planned).
- Projection `api_key` in `02-data-models.md`.

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| CreateAPIKey | command | `POST /v1/api-keys` `{owner_user_id, audience, audience_type?, expires_in_days?, name?}` -> `201 {"key":"jns_...","api_key":{...}}`. **Validation order** (`bindCreateAPIKeyRequest`, then transaction): (1) malformed JSON, `owner_user_id` missing, `audience` missing/empty/> 100 chars, or `name` > 100 chars (runes) -> `400 invalid request`; (2) `owner_user_id` not a UUID (`uuid.Parse`) -> `400 invalid owner`, before any query (**fixed by T28**, decision #9; it was `500 internal error` on Postgres); (3) `audience_type` present and not `"text"` -> `400 invalid audience_type` (absent or `null` -> `text`); (4) `expires_in_days` not an integer in 1..365 (typed as float so `1.5` reaches the range check; `1.0` is accepted as 1) -> `400 expires_in_days must be between 1 and 365` (absent/`null` -> `expires_at = null`; else now + N days); (5) in one transaction: owner unknown or with `disabled_at` -> `400 invalid owner`, else insert. `name` stored as given (not trimmed). No idempotency: two calls create two distinct keys | 400 invalid request · 400 invalid owner · 400 invalid audience_type · 400 expires_in_days must be between 1 and 365 · 401 · 500 internal error |
| ListAPIKeys | query | `GET /v1/api-keys?owner_user_id=&audience=`: both filters optional, exact match, AND-combined; `owner_user_id` bound with `omitempty,uuid`. Revoked and expired keys are included. Order `created_at DESC, id DESC`. `200 {"api_keys":[...]}`; no match -> `[]` (never `null`); no pagination | 400 invalid request (owner not a UUID) · 401 · 500 |
| RevokeAPIKey | command | `POST /v1/api-keys/:id/revoke`. Body optional (empty body = no scope; `{}` ok); `{audience}` scopes the lookup. Order: bind (malformed JSON -> 400) -> id check (non-UUID -> 404) -> scoped lookup (404) -> `UPDATE ... SET revoked_at = now WHERE id = ? AND revoked_at IS NULL` -> reload. **Idempotent**: repeating keeps the original `revoked_at`. `200 {"api_key":{...}}` | 400 invalid request (malformed JSON) · 404 api key not found (unknown id, non-UUID id, audience mismatch; no effect) · 401 · 500 |
| UpdateAPIKey | command | `PATCH /v1/api-keys/:id` rename only. Body `{name?, audience?}`, body required (empty body -> `400 invalid request`). Decoded with `DisallowUnknownFields`: any other field (`key`, `owner_user_id`, `audience_type`, `expires_in_days`, `expires_at`, `revoked_at`, ...), even `null` -> `400 only name is editable`. Malformed JSON, non-string `name`, `name` > 100 chars -> `400 invalid request`. `name` absent, `null` or blank (whitespace only) -> stored `NULL`; otherwise stored literally (not trimmed). `audience` only scopes the lookup like revoke; non-UUID id or unknown id or mismatch -> `404 api key not found`. Revoked/expired keys can be renamed; `key_hash`, `revoked_at`, `expires_at`, owner, audience never touched. `200 {"api_key":{...}}` | 400 only name is editable · 400 invalid request · 404 api key not found · 401 · 500 |
| VerifyAPIKey | query | `POST /v1/api-keys/verify` `{key, audience}` (both required strings; malformed/empty body, missing/`null`/non-string -> `400 invalid request`). Key without `jns_` prefix, unknown, revoked, expired (`expires_at <= now`), owner with `disabled_at`, or audience mismatch -> one identical `401 {"error":"invalid api key"}` (no reason leak). Valid -> `200 {"valid":true,"key_id","owner_user_id","audience"}`. Read-only: one SHA-256 + one indexed `SELECT` joined on `users`, no write | 400 invalid request · 401 invalid api key · 401 (service key) · 500 |

## Invariants
1. Raw API keys are never stored or logged; only the SHA-256 hash is persisted.
2. Every invalid-key case of verify is indistinguishable (status and body).
3. Verify returns identity only (`key_id`, `owner_user_id`, `audience`); no scopes/roles.
4. `/v1/api-keys/*` never opens without a service key; third parties never call Janus.
5. `name` is the only mutable attribute of a key besides `revoked_at`.

## Shared code
`lookup.go` (package-local): `apiKeyIDParam`, `mutateScopedAPIKey`, `changeScopedAPIKey`, `respondAPIKeyMutationError`, shared by revoke and update. See `01-architecture.md`.

## Flow (given / when / then)
- Dado owner válido, Quando create, Então 201 com `key` crua uma vez e só o hash armazenado.
- Dado owner_user_id "abc", Quando create, Então 400 `invalid owner` sem consulta ao banco.
- Dado chave revogada, Quando verify, Então 401 `invalid api key` idêntico a chave desconhecida.
- Dado revoke repetido, Então 200 com o `revoked_at` original.
- Dado PATCH com `audience_type`, Então 400 `only name is editable`.

## Acceptance
- [ ] api: `features/apikeys/*_test.go` (create, create_owner_uuid, list, revoke, update, verify, handler) via `gin.Engine` + SQLite em memória
- [ ] raw key / key_hash nunca em stdout nem em list/revoke/verify
- [ ] regressão T28: owner não-UUID -> 400 `invalid owner`
