# Drills that run

The storage plan (steps 0–7) made backups, snapshots and durable disks the
norm; a backup nobody restored is a hope. These drills are the schedule and
the steps. Results go in the cloud repo's rollout notes
(`docs/plans/2026-10-10-storage-production-rollout.md`) with the date, the
numbers and what was off; `GET /api/readyz` says whether the last platform
backup is fresh, not whether it restores.

## Monthly, on dev: a shared-server restore and one tenant's dump

1. Pick the latest base backup of a shared server:
   `shpyrd-ctl --context oke-shpyrd-dev postgres servers` (the server), then
   `kubectl -n shpyrd-shared-postgres get backups.postgresql.cnpg.io` (the copy).
2. Restore it into a scratch cluster in the same namespace from the
   server's object store (a CNPG `Cluster` with `bootstrap.recovery` from
   the server's `ObjectStore`, 1 instance, the smallest size; the plan's
   `docs/plans/poc` scripts have the shape). Wait for `readyInstances: 1`.
3. `pg_restore` one tenant's nightly dump into it and count rows against
   the live tenant: `psql -At -c 'select count(*) from <table>'` on both.
   The counts match, or the drill failed.
4. Delete the scratch cluster. Record: the backup's age, the time to
   `readyInstances: 1`, the dump's size and restore time, the row counts.

## Quarterly, on dev: a full platform restore

1. `shpyrd-ctl cluster backup` on production stands as the source; copy the
   latest archive to the dev backups bucket.
2. Tear dev down and up (`infra/oci/shpyrd-dev/RUNBOOK.md`), `cluster init`,
   then `shpyrd-ctl cluster restore` from the archive.
3. Check: the control-plane database answers (`shpyrd-ctl workspace
   status`), a project's source archive is readable (`/api/readyz`
   `sources`), a dedicated database restores from its own backups
   (`shpyrd pg restore <name> --as <copy>`), a volume restores from a
   snapshot (`shpyrd volumes restore <name> --from <snapshot> --to <copy>`).
4. Record the wall-clock of each step and anything done by hand.

## After a migration, on production: a planned node drill

Off hours. Before: the other apps node's free allocatable
(`kubectl describe node`) holds every process of the node to drain, and the
autoscaler's maximum leaves room for one more node.

1. `kubectl cordon <apps node>`; `kubectl drain <apps node>
   --ignore-daemonsets --delete-emptydir-data`.
2. Watch the processes come back on the other node (block volumes follow
   their process: `kubectl get pods -A -o wide` and the volumes' attach
   events); note any process that stays Pending and why.
3. Back-out at any point: `kubectl uncordon <apps node>`.
4. Record: the drain's duration, the longest reattach, anything pinned.

## Twice a year, on dev: an unplanned node loss

Terminate an apps node's instance through the node pool (OCI console or
`oci compute instance terminate`), not through Kubernetes. Watch the
processes and their block volumes return on another node, the autoscaler
replace the machine, and the time each took. A node-local volume (the
`shpyrd-local` class) does not survive this, by design: the drill shows
which processes still have one.
