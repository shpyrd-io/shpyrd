# RFC-0039 Postgres pooling, credential rotation and resize

**Status:** implementable

**Owner:** unassigned

**Depends on:** RFC-0009 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-09-22

## Summary

PgBouncer pooling in front of a database (`spec.pooler`), `shpyrd pg rotate db` to change
the application password with a config release for attached apps, `shpyrd pg resize db
--size --storage`, and metrics scraping (PodMonitor) shared with RFC-0028.

## Motivation

Serverless-style apps open many connections; passwords must be rotatable; size changes
need a command.

## Proposal

- `spec.pooler: {enabled: true, instances: 1, mode: transaction}` → CNPG `Pooler`
  `<name>-pooler`; the Binder's host becomes `<name>-pooler-rw` while `DATABASE_DIRECT_URL`
  keeps the primary for migrations.
- `shpyrd pg rotate db`: new random password written to the `<name>-app` Secret (CNPG
  applies it); the App controller watches CNPG app Secrets and re-renders bindings → release
  "Rotate db credentials" in every attached app.
- `shpyrd pg resize db --size shared-l --storage 20Gi`; CNPG rolls instances; single
  instance means a short outage, said in the command output.
- `spec.monitoring.enablePodMonitor: true` always on.

## Design Details

- Secret watch mapped to Apps bound to the Postgres (label `cnpg.io/cluster`).

## Amendment 2026-10-15: what shipped with the shared type

- **Pooling** is a shared server's, not a database's: a CloudNativePG `Pooler` in
  transaction mode per server (`<server>-pooler`, small pools per user and database,
  idle server connections closed within a minute, prepared statements at the protocol
  level), switched on when the seven-day p95 of the server's client connections passes
  60 % of `max_connections` (the metering loop stamps the server; an operator may stamp
  it by hand) and on for good. The tenants' Services then turn to it, and every shared
  database has `DATABASE_DIRECT_URL` (Service `<name>-direct`) at the server itself from
  the first day, for what a transaction pool cannot carry. `spec.pooler` on a dedicated
  database is not implemented.
- **Rotation** is `shpyrd pg rotate <name>` for both types: a shared database's role
  Secret is rewritten and the operator applies it (the stamp `shpyrd.io/rotate`); a
  dedicated database's `<name>-app` Secret gets the new password and its URIs rewritten,
  and CloudNativePG applies it in seconds. The apps bound to it restart with the new
  credentials within a minute. Refused while the database is not Ready.
- **Resize** shipped as `shpyrd pg resize` for dedicated databases; a shared database
  has a cap instead.

## Implementation History

- 2026-09-22: RFC written.
