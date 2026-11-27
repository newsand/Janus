## [2026-11-26] Sync

### Done
- Janus spec rebuilt in the design-docs-specs-creator layout (00-brief, 01-architecture, 02-data-models, 03-features/feature-{auth,users,password,twofa,apikeys,health,mailer}, 04-contracts, 05-non-negotiables, 06-discretion, decisions pointer). Old `auth.md` and `architecture.md` removed; content migrated.
- Every endpoint, status code, body, model field, migration, env var, middleware and package checked against the code.

### Decisions outside the spec
- Code from `latest` already has the mailer (`internal/mailer`, `mail_outbox`, SMTP and SendPulse) and applies SQL migrations on boot. The API-key spec that said the mailer was not implemented was describing an older checkout; this file now matches the code.

### Follow-ups
- `audience_type` second type is still open.
- TBD: split auth/users handler.go per operation (L1 trigger exceeded).
