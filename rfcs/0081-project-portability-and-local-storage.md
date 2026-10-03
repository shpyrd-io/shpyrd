# RFC-0081 Project portability and local storage for the MVP

**Status:** implementable

**Owner:** Patrick Negri

**Creation date:** 2026-10-03

## Decision

Small customers accept a planned maintenance window. Avoid a provider block
volume per project resource: share node disks through isolated local PVCs.
Keep the S3 gateway for object storage. Implement recovery before changing
storage defaults or migrating existing data.

## Phase 1: portable project archive

Workspace/project administrators and cluster operators can download one
`.tgz` containing project configuration, exact deployed release information,
source archives, PostgreSQL logical dumps, and every project volume's files.
No permanent archive catalog or object-store copy is created. Temporary
files used for assembly, validation, and recovery are removed after success.

Project maintenance is durable and distinct from idle sleep or workspace
suspension. It blocks mutations and new application execution, stops web,
workers and scheduled writers, and serves HTTP 503 with Retry-After and
Cache-Control: no-store to every application HTTP method. No redirect to
login and no successful response acknowledging an unprocessed webhook.
Existing in-flight requests drain before data capture. A retrying client
remains responsible for its own delivery policy and idempotency.

Restore validates archive inventory, checksums, sizes, paths and resource
compatibility before touching the project. Namespace, identity and access
are determined by the authenticated target, never by archive metadata.
An incomplete restore must not resume serving. Preserve recovery data until
the restored workloads are healthy. Interrupted operations remain visible
and resumable/recoverable rather than silently clearing maintenance.

Tests cover no data resources, multiple volumes, multiple databases,
release/config preservation, archive truncation/corruption, unsafe paths,
cross-workspace denial, concurrent operations, failed restore and cleanup.

## Phase 2: local disks and project placement

Use an existing local provisioner and node affinity, keeping PVCs and
resource APIs. Nodes with local data cannot be removed automatically.
Applications mounting the same volume remain on its node. Monitor actual
free space; a local PVC request alone is not a filesystem quota.

The operator can see project placement, CPU, memory and disk availability,
stop a project completely, and move selected placement groups to eligible
destination nodes. A group consists of a database or the connected set of
processes and local volumes they share. Application and data node roles
remain distinct: moving a project does not imply co-locating all resources.
A local volume follows its consumers; there is no separate volume-serving
node or network filesystem in this mode. Reject contradictory placements
before stopping anything. A stateless process can move independently.

For the MVP, maintenance still covers the whole project while any group is
moved, including writers on other nodes. Reuse the portable archive engine,
validate destination capacity, preserve source data until destination
validation, and keep HTTP maintenance throughout. Rollback to the original
copy is allowed before releasing maintenance; after new writes are accepted,
returning to an old copy requires another coordinated migration.
Migration of existing production resources is explicit, never an install
side effect. Node/disk loss requires an independently saved recovery copy.

## Scope

No live migration, distributed filesystem, automatic rebalancer, archive
catalog, or replacement of PostgreSQL with a home-grown database service.
Platform backup recipient encryption remains the separate issue #76.

## Placement planning and autoscaling

The console shows current CPU and memory requests for the whole group,
including all observed replicas, init-container peaks, sidecars and pod
overhead. These are reservations, not measured utilization or a forecast of
future autoscaling. Stopped processes without observed pods show unknown
requests rather than zero.

Destinations are filtered by pool and compared using projected free CPU,
memory and disk after the move. The balanced recommendation maximizes the
smaller remaining fraction of CPU and memory among nodes with complete
metrics. Operators can instead sort by free CPU, memory or disk. Current
nodes, different architectures, unhealthy/cordoned nodes, and insufficient
capacity cannot be selected. Unknown measurements remain explicitly unknown;
the backend checks capacity again before pausing and copying.

Data size uses volume metrics when every claim has a measurement. A manual
read-only scan estimates uncompressed file bytes without pausing the project,
reading file contents, or following symlinks. Measurements expire from the UI
after five minutes. Scan helpers mount only the selected group's claims,
read-only, and expire automatically if the API process disappears. This is an
estimate while writers are active; sparse files and metadata can make actual
allocated space or transfer size different. Destination filesystem capacity
is checked directly before copying, with an additional 1 GiB reserve.

Moving the last local volume off an apps node allows scale-down once the
retired source PV is reclaimed. Protection stays while any local PV remains,
including a retained recovery copy. Only shpyrd-owned protection is removed;
operator-owned scale-down protection is preserved. Pool minimums, other
workloads and autoscaler delays still apply. Platform/data pools remain fixed.
Manual process moves currently pin the process to the chosen hostname. This
also constrains future replicas; there is no automatic return to unrestricted
scheduling or automatic data movement when that node fills up.

## Current implementation boundaries

- New project PVCs use `shpyrd-local`; existing provider volumes are preserved.
  The move endpoint currently accepts already-local claims only. Moving a
  provider disk to local storage requires a separate explicit migration.
- Database movement requires a single PostgreSQL instance and one data claim,
  without a separate WAL volume or tablespaces.
- Project archives support PostgreSQL and volumes. Projects containing Redis
  or object-bucket resources are refused rather than producing partial backups.
- Restore preserves the target project's identity, workspace, domains and
  access. It restores the deployed build as a new release; historical release
  records are not reconstructed.
- Local PVC size is a planning request, not an enforced quota. Provider disk
  snapshots and per-volume expansion are unavailable for these claims. Grow
  the node filesystem or move data to a larger node instead.
- Local data does not provide machine-failure recovery. Keep downloaded
  archives outside the cluster. Download tickets expire after five minutes
  and are consumed by one request; interrupted downloads must be prepared again.

## Reproducing validation

`contrib/test-project-portability.sh` creates a separate three-node kind
cluster, installs the CRDs, local provisioner and CloudNativePG, then runs
online volume/database measurement, two-volume/two-database backup and
restore, and independent volume and database moves. It refuses to reuse an
existing cluster with its reserved name and cleans up only the cluster it
created. It requires Go, Docker, kind, kubectl and Helm, plus enough CPU and
disk capacity alongside any existing local clusters. The fixture has no
application image; ingress behavior and image archives have separate tests.

The large archive and real PostgreSQL library tests can run without kind:

```sh
SHPYRD_PROJECT_ARCHIVE_LARGE_TEST=1 SHPYRD_PROJECT_POSTGRES_TEST=1 \
  go test ./pkg/projectarchive -count=1 -v -timeout=10m
```

The PostgreSQL tests require `initdb`, `pg_ctl`, `psql`, `pg_dump` and
`pg_restore` on PATH. They create disposable servers on private Unix sockets
and ignore existing PG connection environment variables. The large test
writes and restores 1.125 GiB and checks the result's hash and heap use.
