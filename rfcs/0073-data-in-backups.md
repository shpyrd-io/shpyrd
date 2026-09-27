# RFC-0073 Data in backups: databases and volumes leave the cluster

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0037 (implemented), RFC-0038 (implemented), RFC-0046 (implemented),
RFC-0060 (implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

The platform backup (RFC-0037) carries the platform's objects and the control-plane store,
and restores a project that can run again — with an empty database. Postgres archives
(RFC-0038) live in the cluster's own object store and die with the cluster; volumes are
recreated empty. This RFC puts the data where the backup is: every project database's
archive replicated to the provider's bucket nightly and on demand, the control-plane
database too, volume snapshots referenced and, where the provider allows, copied; restore
puts the data back per project, per database, to a point in time. A workspace owner can
also take their data home: an export with the databases' dumps.

## Motivation

RFC-0037's implementation status is explicit: *"Database and volume contents. Postgres
archives live in the cluster's own object storage and die with the cluster: a database
restored on a fresh cluster starts empty."* For the people this platform is for — who chose
it so that "is it backed up" is not their problem — this is the one gap that matters.
RFC-0033's Story 2 adds the second reason: a customer's data must not be hostage to the
cluster, the partner or the platform.

### Goals

- A cluster lost entirely is restored on a fresh one with every project's data as of the
  last archive: `shpyrd-ctl cluster restore` puts databases back, not just objects.
- A project's database restored to a point in time on the same cluster
  (`shpyrd pg restore --at 2026-09-27T10:00`) — RFC-0038 does this within the cluster;
  now from the bucket.
- The control-plane database (people, roles, tokens) is in the same archive, restorable the
  same way.
- A workspace owner exports their workspace with database dumps (`shpyrd workspace export
  --with-data`): a directory of dumps and a manifest, encrypted with a key they hold.
- Nothing in the archive is readable without the backup key (RFC-0037's).

### Non-Goals

- Continuous replication to a second cluster (disaster recovery with RPO of seconds).
- Backups of an app's own files outside volumes and databases.
- Redis contents: caches and queues are recreated (open question 3).

## Proposal

- **Postgres archives to the provider's bucket.** CloudNativePG's Barman Cloud plugin
  (RFC-0038) writes base backups and WAL to the in-cluster Garage today; add a second
  destination: the platform backup bucket (RFC-0037's `SHPYRD_BACKUP_TARGET`), under
  `databases/<workspace>/<project>/<database>/`, with RFC-0037's retention. Either Barman
  writes to both (two object stores in the plugin configuration), or a nightly job copies
  the in-cluster archive to the bucket (open question 1). Point-in-time restore from the
  bucket becomes possible on any cluster with the key.
- **The control-plane database** joins the same scheme (it is a CNPG cluster too, RFC-0033
  phase 1): archived to `databases/_platform/control-plane/`.
- **Volumes.** The nightly backup records each volume's latest snapshot id (RFC-0060 takes
  snapshots) in the archive manifest; on providers where snapshots can be copied to object
  storage (AWS EBS → S3 through the snapshot API, OCI block volume backups), the job copies
  them; elsewhere the restore documents that the snapshot must exist in the provider
  account.
- **Restore.** `cluster restore` restores databases after their Postgres resources exist
  (CNPG recovery from the bucket, `recoveryTarget` optional), then apps; `cluster restore
  --project shop` includes the project's databases. Restoring a database as a copy for
  inspection (`shpyrd pg restore --as shop-db-copy`) reuses RFC-0038's flow with the
  bucket as source.
- **Workspace data export.** `GET /api/workspace/export?data=true` (owners) produces a job:
  `pg_dump` of each database of the workspace's projects (through the platform, never
  exposing the database), volume tarballs up to a size cap, the RFC-0037 workspace dump,
  a manifest; the archive is encrypted with a key shown once to the owner and left in the
  bucket for 24 hours for download through a signed URL. Audited.

## Design Details

- Credentials: the backup bucket's credential is the CronJob's (RFC-0037); CNPG's plugin
  needs one per Postgres resource — a bucket-scoped credential the Postgres extension
  provisions from the platform's (RFC-0046's per-consumer keys) with a prefix policy.
- Encryption: Barman's archives are encrypted at rest by the provider; the export archive
  and the manifest use RFC-0037's age-style key.
- Retention: databases follow `SHPYRD_BACKUP_KEEP`; WAL retention bounded by the oldest
  base backup kept.
- Costs: one more copy of every archive in the provider's bucket; the plan (RFC-0042) may
  cap storage.
- Tests: the RFC-0037 restore loop extended with a database that has a row; e2e on a cloud
  profile, by hand first.

## Open questions

1. Barman writing to two object stores, or a copy job from the in-cluster store? Default:
   **two destinations in Barman** (no second pipeline to fail silently).
2. Volume snapshot copies in the first version, or references only? Default:
   **references**, copies where the provider makes it a single API call (OCI, AWS).
3. Redis: dump RDB into the archive? Default: **no** (caches and queues), unless a Redis
   resource is marked `durable` (a follow-up of RFC-0040).
4. Data export by owners in this RFC or its own? Default: **this RFC**: it is the same
   machinery pointed at a person.

## Implementation History

- 2026-09-27: RFC written (research; replaces the reference "RFC-0068 dumps in backups" in
  RFC-0033, whose number went to signed-in detection on identified apps).
