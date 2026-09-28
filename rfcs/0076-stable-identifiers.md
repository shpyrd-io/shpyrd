# RFC-0076 Stable identifiers: IDs identify, names present

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (in progress: workspaces, addresses, the edge), RFC-0059
(implemented: in-cluster registry, base36 repository paths), RFC-0075 (in progress:
usage ledger)

**Amends:** RFC-0033 (address change), RFC-0003 (project namespaces), RFC-0075 (ledger
keys)

**Creation date:** 2026-09-28

**Last update:** 2026-09-28

---

## Summary

Every workspace and every project gets one identifier that never changes and that no
person is expected to read. Everything Kubernetes and the ledger key on — namespaces,
custom resource names, workload names, labels, registry paths, usage and cost rows — uses
that identifier. Everything a person reads — the slug in the hostname, the display name,
the address — is a mutable label that can be renamed with a field change.

Today the platform does this for the registry (`apps/<base36 workspace id>/<project>`)
and for the ledger's workspace column (`workspace_id uuid`), and does the opposite
everywhere else: namespaces are `app-<workspace slug>-<project slug>`, the App custom
resource is named by the project slug, the `shpyrd.io/workspace` and `shpyrd.io/project`
labels carry slugs and are called authoritative, and `usage_buckets.project` is the slug
as text. Because namespaces and resource names are immutable in Kubernetes, that made
the slugs immutable too, and RFC-0033 introduced a separate renameable *address* to
compensate. The address mechanism is sound (it is how hostnames move); making it carry
the weight of identity was the mistake. This RFC moves identity down to IDs so that the
slug becomes just another label.

## Motivation

- **A rename should be one field.** Renaming the `demo` workspace to Acme today changes
  the address to `acme.shpyrd.app` but leaves `demo` as the slug: in `kubectl get ns`,
  in `shpyrd-ctl economics`, in the Economics card, in the registry cache path. The
  Cluster page shows a workspace called `demo` whose address is `acme.shpyrd.app`.
  Projects cannot be renamed at all — `shpyrd projects rename` changes the display
  name only, because the project slug is the namespace, the App name and the workload
  names.
- **Billing history must survive renames.** `usage_buckets.workspace_id` is a UUID, which
  is why the workspace rename kept the ledger continuous. `usage_buckets.project`,
  `cogs_buckets.project` and `sleep_events.project` are the slug as text. The day project
  slugs become renameable, a customer's invoice history splits at the rename — and ledger
  rows are immutable by design, so it cannot be repaired afterwards. This has to be right
  before the first invoice.
- **The registry already got it right.** RFC-0059 chose base36 workspace IDs for
  repository paths precisely so a workspace could be renamed without re-pushing images.
  The rest of the platform should follow the convention it already has.

## Design

### Identifiers

| Object | ID | Where it lives | Rendered as |
| --- | --- | --- | --- |
| Workspace | UUID (exists) | `workspaces.id` | `ids.Short(id)` — 25 base36 chars |
| Project | UUID (new) | generated at create, stored on the App (`spec.id`, label) and used as its name | `ids.Short(id)` |

`pkg/ids` (RFC-0059) renders a UUID as 25 lowercase base36 characters: valid in DNS
labels, OCI repository names, Kubernetes names and labels. Both IDs use it.

### Names

| Kubernetes object | Today | After |
| --- | --- | --- |
| Project namespace | `app-<ws slug>-<proj slug>` / `app-<proj slug>` | `p-<proj id>` |
| App custom resource | `<proj slug>` | `<proj id>` |
| Deployment, Service per process | `<proj slug>-<process>` | `<process>` (`web`, `worker`) |
| Ingress | `<proj slug>`, `<proj slug>-edge` | `app`, `app-edge` |
| kpack Image | `<proj slug>` | `<proj id>`; `spec.tag` = `apps/<ws id>/<proj id>` |
| Build cache image | `build-cache/<ws slug>/<proj slug>` | `build-cache/<ws id>/<proj id>` |
| Postgres, Redis, Volume, Bucket CRs | user-chosen name | unchanged — these names are the user's, inside the project's namespace |

The namespace carries the workspace only as a label: one project, one namespace, one
ID. `p-<25 chars>` is 27 characters, well inside the 63-character limit and leaves room
for CNPG's `<name>-rw` style suffixes on resources inside it.

### Labels and annotations

Authoritative (used by controllers, metering, network policy, the edge):

```
shpyrd.io/workspace-id: <ws id>
shpyrd.io/project-id:   <proj id>
shpyrd.io/process:      web            # unchanged
```

Display (for people reading `kubectl`, and for `kubectl get ns -l shpyrd.io/project=shop`
to keep working):

```
shpyrd.io/workspace: <ws slug>        # updated on rename
shpyrd.io/project:   <proj slug>      # updated on rename
```

Labels are mutable; a rename updates the display labels on the namespace and on every
labelled object in it. Nothing selects on display labels. NetworkPolicy namespace
selectors, the KEDA route, the pod-labels allowlist and Prometheus joins move to the
`-id` labels.

### The ledger

`usage_buckets.project`, `usage_hourly.project`, `cogs_buckets.project`,
`sleep_events.project` become the project ID (text, the `ids.Short` form — the ledger
never joins to Kubernetes, it only needs a stable key). The preview and the CLI resolve
IDs to the current slug and name at read time, the way `QueryBuckets` already resolves
`workspace_id` to a slug. Historical rows show the project's *current* name — a renamed
project's whole history appears under its new name, which is what a customer expects
from an invoice.

The metering loop's namespace mapping reads `shpyrd.io/workspace-id` and
`shpyrd.io/project-id` off the namespace and stops resolving slugs. The OpenCost COGS
writer parses the project ID from the namespace name (`p-<id>`) and looks the workspace
up by ID; the slug-prefix matching of v0.9.24–25 goes away.

### Hostnames and renames

The project slug is the hostname label: `<proj slug>.<workspace address>`. Renaming a
project changes `spec.slug`, the display labels, the hostname and its certificate; the
old hostname becomes a `moved` host with a 301 for 30 days — exactly the RFC-0033
address-change machinery, now applied to projects too. Renaming a workspace changes its
slug and address the same way. The slug/address split of RFC-0033 collapses: the
address is the workspace's hostname and the slug is its label; both mutable, neither
identity.

`shpyrd projects rename <slug> <new-slug>` renames the slug (hostname); `--name` sets the
display name. Uniqueness is per workspace, checked against slugs, moved hosts and
reserved names as today.

### API and CLI

The API keeps taking slugs in paths (`/api/projects/:slug`) — they are what people type —
and resolves them to the App by an index on `spec.slug` within the workspace. Responses
carry both `id` and `slug`. Lists shown to people (Cluster page, `shpyrd-ctl economics`,
`shpyrd projects list`, the launcher) show name and address/hostname; the slug appears
where it is the thing being typed; the ID appears only in `--wide` output and tooltips.

## Migration

Namespaces and resource names cannot be renamed in place. Two modes coexist during the
transition:

1. **New projects** are created with IDs from the first release of this RFC. The
   controller derives every name from `spec.id` when present.
2. **Legacy projects** (`app-<slug>` namespaces, slug-named App) keep working: the
   controller reads names off the object rather than recomputing them, the `-id` labels
   are stamped on their existing namespaces and objects, and the ledger's `project`
   column is backfilled from slug to ID for rows that map to a live project. Legacy
   projects cannot be renamed until migrated.
3. **`shpyrd projects migrate <slug>`** recreates a legacy project under its ID: new
   namespace, App, workloads and Ingress; volumes moved by snapshot and restore
   (RFC-0060), databases by backup and restore (RFC-0038), secrets and config copied;
   hosts switched with the old namespace's Ingress removed last; the old namespace
   deleted after a soak. Downtime is the volume/database restore window; the command
   says so and asks.

The first cloud has no customers: its example projects are recreated rather than
migrated, and the ledger is truncated once more before the first invoice.

## Implementation status

Not started. Scheduled at the start of the phase after RFC-0075's economics work, before
Postgres sleep and the workspace-level sleep defaults, which would otherwise be written
against slug-keyed identity.

## Open questions

1. **Project ID storage**: `spec.id` on the App (the CR name is derived from it, so it is
   redundant but explicit) versus relying on `metadata.name` alone. Default: **both** —
   the name is the ID; `spec.id` makes intent readable and survives a future move to a
   projects table in the store.
2. **A `projects` table in the store**: today projects exist only as CRs. Billing,
   invitations and audit reference them by slug. A table (id, workspace_id, slug, name,
   created_at) would let the store resolve IDs without the API server and record deleted
   projects for invoices. Default: **add it** with this RFC; the controller mirrors the CR
   into it.
3. **Namespace prefix**: `p-<id>` versus `app-<id>`. `app-` is what operators know, but
   the slug rule forbids slugs starting with `app-` because of today's naming; with IDs
   that reservation could be dropped. Default: **`p-`**, and drop the `app-` reservation
   for new slugs once no legacy namespace remains.
4. **Display labels at all**: keep `shpyrd.io/workspace`/`project` as mutable display
   labels, or remove them and rely on `shpyrd projects list`. Default: **keep** — they
   cost nothing and make `kubectl` legible.

## Relationship to other RFCs

- **RFC-0033**: the address change (v0.9.16) stays as the hostname-move mechanism and
  gains project renames; the slug/address distinction as *identity* is retired.
- **RFC-0059**: the registry's base36 workspace paths are the precedent; project paths
  join them.
- **RFC-0075**: the ledger's `project` columns become IDs; the metering and COGS writers
  read `-id` labels. Rows written before this RFC on the first cloud are truncated.
- **RFC-0003**: project namespaces are named by ID.

## History

- 2026-09-28: written after the first workspace rename on the cloud showed `demo` in the
  Cluster page next to `acme.shpyrd.app`, and the review found `usage_buckets.project`
  keyed by slug.
