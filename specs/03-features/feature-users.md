# Feature: Users and invites

## Source
Janus code `features/users/handler.go`; migrated from `janus/specs/auth.md` §2, §9, §11, §15 (old layout, removed).

## Description
User CRUD behind a service key (machine secret; there is no per-user admin role and CRUD never opens with a user JWT) and the invite flow for closed signup. Accounts are created by CRUD, by accepting an invite, or by seed/internal API of the product.

## Depends on
- feature-mailer (invite delivery: NOT implemented in this checkout; stub mode returns the token in the response)

## Rules
- `nome` never blank (trimmed on create/update). Passwords >= 8 chars wherever set (create, invite accept, reset); bcrypt cost 12.
- Disable = soft via `disabled_at`; disabling or changing `email` revokes all the user's refresh tokens; `nome`, `telefone`, `cpf` revoke nothing. Re-enabling revokes nothing. There is no standalone revoke endpoint.
- `GET /v1/users` is paginated: 100 per page (`UsersPageSize`), `created_at DESC, id DESC`, no search.
- Rate limit: `POST /invites/accept` only (not `/users`, not `POST /invites`).

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| CreateUser | command | `POST /v1/users` (service key) `{email, nome, password?, telefone?, cpf?}`. Order: bind (`email` required+valid, `nome` required) -> trim `nome`, blank -> cpf check -> email uniqueness -> optional password (non-empty must be >= 8) -> insert. `201 {"id","email","nome","created_at"}`. Without `password` the user has none until an invite is accepted | 400 `{"error":"invalid request","details":<msg>}` (bind; `nome must not be blank`; `cpf invalid`) · 400 `{"error":"password must be at least 8 characters"}` · 409 `email already exists` · 500 internal error · 401 |
| ListUsers | query | `GET /v1/users?page=N` (service key); `page` default 1; `200 {"users":[{id,email,nome,telefone,cpf,"2fa_enabled",created_at,updated_at,disabled_at}],"page","page_size":100,"total","total_pages"}`; past the last page -> empty `users` | 400 `invalid page` (non-integer or < 1) · 500 · 401 |
| GetUser | query | `GET /v1/users/:id` (service key) -> `200` same fields as the list item. Any lookup failure (unknown or malformed id) is `404` | 404 `user not found` · 401 |
| UpdateUser | command | `PATCH /v1/users/:id` (service key) `{email?, nome?, telefone?, cpf?, disabled_at?}`. Order: bind -> load (404) -> email change (409 if used by another; format not validated) -> `nome` trim/non-blank -> `cpf` -> `disabled_at`: non-empty string disables (stores now; the value itself is ignored), `""` re-enables. Applies updates; if disabling or email changed, revokes all refresh tokens. `200 {"message":"user updated"}` (also when nothing changed) | 400 invalid request (bind) · 400 invalid request + details (`nome must not be blank`, `cpf invalid`) · 404 user not found · 409 email already exists · 500 · 401 |
| CreateInvite | command | `POST /v1/invites` (service key) `{email}` (required, valid). Sets all unused invites of that email used, creates a new one-shot invite (opaque token, hash only, TTL `INVITE_TTL`, default 168h). `201 {"message":"invite sent","email":"..."}`; with `MAILER_STUB=true` also `token` and `expires_at` and the token is logged. With `MAILER_STUB=false` nothing is returned or delivered in this checkout | 400 invalid request · 500 internal error · 401 |
| AcceptInvite | command | `POST /v1/invites/accept` (public, rate limited) `{token, password (min 8), nome?}`. Token resolved with `FindUsableToken`. Existing user with a password -> 409. Existing user without password -> sets `password_hash` and `nome`. No user -> creates one. `nome` blank after trim -> invite email. Marks invite used. `200 {"message":"account activated","user_id":"<uuid>"}`; does not log the user in | 400 `{"error":"invalid request","details":<msg>}` · 401 `invalid or expired invite` · 409 `account already exists` · 500 · 429 |

## Flow (given / when / then)
- Dado convite válido para e-mail sem usuário, Quando accept, Então usuário criado, convite usado, 200.
- Dado convite já usado, Quando accept, Então 401 `invalid or expired invite`.
- Dado PATCH com novo email, Quando concluir, Então todos os refresh tokens revogados.
- Dado PATCH `disabled_at:""`, Então conta reabilitada sem tocar tokens.

## Acceptance
- [ ] api: integração via `gin.Engine` + SQLite em memória (`features/users/handler_test.go`, `get_update_characterization_test.go`)
- [ ] paginação: 100/página, ordem estável, `page` inválido 400, além da última -> lista vazia
- [ ] sem convite público: sem token ninguém entra; até aceitar, login por senha falha e magic link não envia
