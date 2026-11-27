# Decisions (Janus root)

Append-only decisions live in the workspace file `teste-login/specs/decisions.md` (single source, not duplicated here). Those that shape this folder:

| # | applies to |
|---|---|
| 2 | API key edit = rename only (feature-apikeys, 04) |
| 3 | quality gate on for janus only (05) |
| 5 | 2FA warnings left as they are (feature-twofa) |
| 6 | docs use placeholders; gitleaks without allowlist for docs (05) |
| 7 | duplication <= 3% by refactor, characterization tests (01, 05) |
| 8 | coverage floor 50% whole module via `-coverpkg` (05) |
| 9 | non-UUID `owner_user_id` -> `400 invalid owner` (feature-apikeys; task T28) |
| 11 | semgrep exclusions only for two rules in `*_test.go` (05) |
| 12 | forgot-password always generic `200` (feature-password) |
| 13 | semgrep container findings ignored for `Dockerfile` and `docker-compose.yml` only (05) |
