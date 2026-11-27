# Feature: Two-factor authentication (TOTP)

## Source
Janus code `features/twofa/handler.go`; `janus/specs/auth.md` §10 (old layout, removed); workspace decision #5 (2FA warnings stay as they are).

## Description
Optional TOTP 2FA (`pquerna/otp/totp`). With `two_fa_enabled=false` (default) login is unchanged. With it `true` and a stored secret, password login returns a challenge instead of tokens; a valid TOTP code then yields the session.

## Depends on
- feature-auth (`auth.RespondWithNewSession`, `auth.CurrentUser`, challenge creation at login)

## Rules
- Challenge: opaque, hash only in `two_fa_challenges`, TTL **5 minutes**, one per user (a new one replaces the previous), deleted on successful verify.
- Enabling only after a valid code, so a mistyped setup cannot lock the account.

## Operations (api)
| name | kind | does | errors |
|---|---|---|---|
| Verify | command | `POST /v1/auth/2fa/verify` `{challenge_token, code}` (public, rate limited): challenge lookup; expired/unknown -> `401 invalid or expired challenge`; user with `two_fa_enabled=true` not found -> `401 2FA not enabled`; secret missing or code invalid -> `401 invalid code`; success deletes the challenge and answers the token response (new family) | 400 invalid request · 401 · 429 |
| Enable | command | `POST /v1/me/2fa/enable` (JWT). Already enabled -> `400 2FA already enabled`. Body without a valid `{code}` (missing code, or any bind failure): generates a TOTP secret (base32, no padding), stores it in `two_fa_secret`, `200 {"secret","message":"scan with authenticator app, then POST code to confirm"}` (each such call replaces the stored secret). With `{code}`: no stored secret -> `400 setup 2FA first by calling without code`; invalid code -> `401 invalid code`; valid -> `two_fa_enabled=true`, `200 {"message":"2FA enabled"}` | 400 · 401 · 500 internal error · 401 (JWT) |
| Disable | command | `POST /v1/me/2fa/disable` (JWT) `{password?, code?}`: bind error or both empty -> `400 password or code required`; not enabled -> `400 2FA not enabled`; at least one of password (bcrypt) or code (TOTP) must verify, else `401 invalid credentials`; clears `two_fa_enabled` and `two_fa_secret`; `200 {"message":"2FA disabled"}` | 400 · 401 · 500 internal error |

## Known limitations (kept intact; do not enable 2FA in production yet)
1. A disabled user can still obtain tokens through the 2FA verify step (Verify does not check `disabled_at`).
2. A TOTP code can be replayed within its validity window.
3. There is no per-challenge attempt cap (only the per-IP rate limit).

These are characterized by tests as current behavior but are not blessed behavior (`specs/05-non-negotiables.md` workspace note: known limitations are not fixed in tests).

## Flow (given / when / then)
- Dado 2FA ligado, Quando login com senha correta, Então `requires_2fa` + `challenge_token`; Quando verify com código válido, Então 200 tokens.
- Dado enable sem code, Então segredo gerado e 2FA ainda desligado; Quando enable com code válido, Então ligado.

## Acceptance
- [ ] api: `features/twofa/handler_test.go` (SQLite em memória)
- [ ] limitações 1-3 documentadas e inalteradas até decisão do desenvolvedor
