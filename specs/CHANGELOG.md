## [2026-11-26] Sync

### Done
- Janus spec rebuilt in the design-docs-specs-creator layout (00-brief, 01-architecture, 02-data-models, 03-features/feature-{auth,users,password,twofa,apikeys,health,mailer}, 04-contracts, 05-non-negotiables, 06-discretion, decisions pointer). Old `auth.md` and `architecture.md` removed; content migrated.
- Every endpoint, status code, body, model field, migration, env var, middleware and package checked against the code.

### Decisions outside the spec
- Code wins over the old docs: no mailer/outbox, no embedded migration runner, magic-link/forgot/invite take only `{email}`, magic-link and forgot are public (rate limited). Upstream mailer content kept as reference in feature-mailer (NOT implemented).

### Follow-ups
- TBD: port the upstream mailer or keep the stub.
- TBD: `audience_type` second type.
- TBD: split auth/users handler.go per operation (L1 trigger exceeded).
