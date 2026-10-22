# RFC-0009 Postgres resource

**Status:** implemented (with gaps) — see Implementation status below

**Creation date:** 2026-09-22

**Last update:** 2026-09-22

## Summary

A `Postgres` project resource backed by the **CloudNativePG** operator (`postgres`
extension): create a database with a size and storage, attach it to an app and get
`DATABASE_URL`; backups to object storage, point-in-time recovery, connection pooling.

## Motivation

Almost every application needs a relational database; provisioning and wiring one is the
first thing people do manually after deploying.

### Goals

- `shpyrd pg create db` to `DATABASE_URL` in the app in one command.
- Production behaviour available when wanted: replicas, backups, PITR, pooling.
- Works on the local profile (MinIO for backups) and on cloud profiles (S3, RDS later).

### Non-Goals

- Multi-tenant shared database servers (one CNPG cluster per resource).
- Managed cloud databases (RDS) in the first version; a later `postgres-rds` extension can
  implement the same resource type.

## Proposal

| Operator | Notes |
| --- | --- |
| **CloudNativePG** (chosen) | Kubernetes-native, actively maintained, declarative `Cluster`, streaming replication and failover, backups to S3-compatible storage with PITR, PgBouncer `Pooler`, arm64 images |
| Zalando postgres-operator | mature, Patroni-based; heavier, older API style |
| Crunchy PGO | solid, larger footprint, commercial backing |
| StackGres | feature-rich, heavy |

```yaml
apiVersion: shpyrd.io/v1alpha1
kind: Postgres
metadata: { name: db, namespace: app-shop }
spec:
  version: "17"
  size: shared-m          # instance size from the catalog
  storage: 10Gi
  instances: 1            # 2-3 for HA
  backups:
    schedule: "0 3 * * *"
    retention: 14d
status:
  phase: Ready
  endpoint: db-rw.app-shop.svc:5432
```

The controller renders a CNPG `Cluster` (and optionally a `Pooler`); CNPG creates the app
user and its Secret; the `Binding` exposes `DATABASE_URL`, `PGHOST`, `PGPORT`, `PGUSER`,
`PGPASSWORD`, `PGDATABASE` (prefix configurable). Backups go to the object-storage
extension (MinIO locally, S3 on AWS).

CLI: `shpyrd pg create|list|info|psql|backup|restore|delete`. Dashboard: status,
endpoint, size, storage used, backups, "Attach".

## Design Details

- Extension component: CNPG operator chart (runlevel after cert-manager), plus the
  object-storage extension as a dependency for backups.
- Size mapping: instance size → CNPG `resources`; storage → PVC size (expandable).
- Credentials rotation: `shpyrd pg rotate db` regenerates the app password and produces a
  config release for bound apps.
- Deleting a Postgres resource requires typing its name and refuses while bound; the last
  backup is kept for the retention period.

### Drawbacks

- The operator is another component (~200 MB); enabled only when the extension is on.

## Implementation History

- 2026-09-22: RFC written; phase E.
- 2026-09-22: Implemented as the `postgres` extension: the CloudNativePG operator (chart
  0.29.0, runlevel rc2) as its component, a `Postgres` CRD (version, size, storage, instances)
  rendered into a CNPG `Cluster` (initdb bootstrap of database and owner `app`, resources
  from the size catalog raised to a 256 MiB memory floor because initdb OOMs below it,
  storage that grows but cannot shrink, superuser access off), status from the cluster's
  ready instances and the `<name>-app` Secret, endpoint `<name>-rw.<ns>.svc:5432`, a
  `Binder` exposing `DATABASE_URL|HOST|PORT|USER|PASSWORD|NAME`. `shpyrd pg
  create|list|info|psql|delete` (delete refused while attached), `shpyrd attach|detach`, the
  generic resources API and dashboard forms. The project network policy now admits every
  platform namespace so the operator can reach its instances. Not done: backups to object
  storage, PITR, `Pooler`, `shpyrd pg rotate`, RDS variant.

## Amendment 2026-10-15: two kinds of database

`spec.type: shared | dedicated` (storage plan, step 3). **Dedicated** is the
cluster per resource described above. **Shared** is a database and a role on a
platform-owned CloudNativePG server (`pg-shared-<n>` in `shpyrd-shared-postgres`,
two instances on the data pool, PostgreSQL 17, 200 databases per server, the next
server opened when one fills): always on, no `size`, `instances`, `version` or
`sleep` (the CRD refuses them), `spec.storage` a logical cap metered every five
minutes (status at 80 %, connections paused at 150 % until under 95 %, sessions
read-only after seven days over). The tenant's names are hashed
(`db_<hash>`, `u_<hash>`, the hash of the Postgres UID recorded on the object so a
platform restore finds them again); the project keeps a Service and a Secret named
after the database, so `DATABASE_URL` keeps its shape with `sslmode=require` and
`connect_timeout=10`; a second Service `<name>-direct` and `DATABASE_DIRECT_URL`
reach the server itself. The workspace's plan says which types it allows and the
default (`settings.limits.postgres`); the type changes only by `shpyrd pg convert
<name> --to dedicated|shared`, which moves the rows under the database's name
(to dedicated: the cluster takes the shared names and credentials; to shared: new
names, one config release), connections paused while a Job copies and counts them,
a copy of the source kept seven days, and puts the database back as it was when it
cannot land. Nobody but the owner connects to a shared database; pgvector is
installed in every one, PostgreSQL's trusted extensions are the owner's to create,
the rest are a dedicated database's. Deletion closes the role, takes a last copy and
drops the data seven days later. Docs: `content/docs/databases.mdx`.

- 2026-10-22 (the storage plan of 2026-10-10 (shpyrd-cloud `docs/plans/2026-10-10-storage-durability-and-scale.md`), steps 3–7): `spec.type: shared | dedicated` (step 3): a shared
  database is a database and a role on a platform-owned server with a cap, always
  on; a dedicated one keeps this RFC's cluster, on block storage, with backups on
  by default. `shpyrd pg convert` moves between them. Databases count against the
  workspace's ceiling (step 4). A dedicated database on the workspace's sleep
  default hibernates (step 6, RFC-0075), is copied before it sleeps, and reads its
  policy back on the status; its `status.endpoint` is the host apps get. The wake
  ports run 15000–24999 (step 7).

## Implementation status

Audited on 2026-09-25 against the code. What the text promises but the platform does not do yet is listed here; superseded means a later RFC decided otherwise and the text above is history.

- **Not implemented:** Typing the database name to delete it (`--yes` / a confirm dialog today); storage *used* on the dashboard.
