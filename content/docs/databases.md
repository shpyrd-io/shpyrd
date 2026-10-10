---
title: Databases and caches
description: PostgreSQL databases (CloudNativePG) and Redis-compatible stores (Valkey) as project resources, attached to apps as config vars.
---

A project can have PostgreSQL databases (run by the CloudNativePG operator) and Valkey or Redis caches and queues. Attach one to the app and it appears as `DATABASE_URL` or `REDIS_URL`. {% .lead %}

On shpyrd cloud, add them from the project's **Resources** page: its **Add resource** menu creates a database or a store, and **Attach** hands it to the app; `shpyrd attach` and `shpyrd detach` work from the CLI as well ([Attaching](#attaching)).

{% callout title="Self-hosted" %}
On a cluster you run, two extensions add the data stores. The `shpyrd pg` and `shpyrd redis` commands then work the same way, signed in with `shpyrd login`, or through a kubeconfig with `--context` (see the [CLI reference](/docs/cli)):

```shell
shpyrd-ctl extensions enable postgres
shpyrd-ctl extensions enable redis
```
{% /callout %}

## PostgreSQL

```shell
shpyrd pg create db --project shop                        # PostgreSQL 17, 5Gi (50Gi on Oracle Cloud, the block volume minimum), 1 instance, size shared-s (128Mi)
shpyrd pg create db --project shop --size shared-m --storage 20Gi --instances 3   # HA with replicas
shpyrd pg resize db shared-l --project shop               # another size; the instances restart one at a time
shpyrd pg list --project shop
shpyrd pg psql db --project shop -- -c 'select version()'
shpyrd pg delete db --project shop --yes                  # refused while attached (or --force)
```

Each database is its own [CloudNativePG](https://cloudnative-pg.io) cluster in the project namespace: streaming replication and failover when `--instances` is 2 or 3, a `db-rw` service for the primary and `db-ro` for replicas, a database `app` owned by user `app`. The instance size sets CPU and memory; The data volume is a block volume on shpyrd cloud and the cloud profiles (50 GiB on Oracle Cloud, the provider minimum; a smaller request is rounded up and the status says so) and sits on the node's own disk on the local profile, like every project volume ([Volumes](/docs/resources#volumes)); the database's backups are what protect it.

### Sizes

Databases have a list of sizes of their own, as Heroku's add-ons have plans of their own: the names are the processes' (`shpyrd sizes list` shows the three lists), but a Postgres `shared-s` is not a process `shared-s`. A database takes only a Postgres size, and gets the smallest when it names none. Each size sets the CPU, the memory, the connections and PostgreSQL's settings for them:

| Size | CPU | Memory | Connections | `shared_buffers` | `work_mem` |
|---|---|---|---|---|---|
| `shared-s` (default) | 0.5, shared | 128 MiB | 20 | 16MB | 1MB |
| `shared-m` | 0.5, shared | 256 MiB | 40 | 64MB | 1.2MB |
| `shared-l` | 1, shared | 512 MiB | 80 | 128MB | 1.2MB |
| `shared-xl` | 2, shared | 1 GiB | 120 | 256MB | 1.6MB |
| `dedicated-s` | 1 | 1 GiB | 120 | 256MB | 1.6MB |
| `dedicated-m` | 2 | 4 GiB | 200 | 1GB | 3.8MB |
| `dedicated-l` | 4 | 8 GiB | 300 | 2GB | 5.1MB |
| `dedicated-xl` | 8 | 16 GiB | 400 | 4GB | 7.7MB |
| `dedicated-2xl` | 16 | 32 GiB | 500 | 8GB | 12.3MB |

From 256 MiB up, `shared_buffers` is a quarter of the memory, `effective_cache_size` three quarters, `maintenance_work_mem` a sixteenth (at most 2GB) and `work_mem` what remains shared by four sorts or hashes per connection. Under 256 MiB, `shared-s` keeps the settings measured for it (`maintenance_work_mem` 16MB, `effective_cache_size` 48MB, `wal_buffers` 1MB, `autovacuum_work_mem` 16MB) and a patient liveness probe: a small app's database, a Prisma or Rails migration with its `CREATE INDEX`, a web process with a pool of 5 to 10 connections; not a reporting database. A few connections of each size are kept for the platform (17 of 20 are the app's).

How they were chosen: each size up to 1 GiB ran PostgreSQL 17 in its memory, less what CloudNativePG's instance manager keeps, with pgbench on every connection the app may open, a write load and then hash-and-sort queries on all of them while an index was built. None was killed for memory; the peaks were 86% (`shared-s`), 87% (`shared-m`), 57% (`shared-l`) and 41% (`shared-xl`) of what was left.

`shpyrd pg resize`, or **Resize** on the project's **Resources** page, gives a database another size of the list; its instances restart with it one at a time, so a database with one instance is unavailable for a moment. Memory goes down only when the size does. The workspace's memory and CPU limits count databases with the processes: a database or a resize that would pass them is refused, and so is a process scaled into memory a database holds.

A database made before databases had sizes of their own is given the Postgres size with the CPU and memory it runs with (one that named no size ran 256 MiB: `shared-m`), and restarts once with that size's settings: its connections become the size's.

### Backups and point-in-time recovery

With the `object-storage` extension enabled ([Extensions](/docs/extensions#object-storage)), a database can be backed up continuously: WAL archiving plus a daily base backup into a bucket of the platform's store that only this database's key can open, kept for the retention period.

```shell
shpyrd pg create db --project shop --backups --retention 14d       # or later:
shpyrd pg backups enable db --project shop --retention 7d --schedule "0 3 * * *"
shpyrd pg backups list db --project shop                           # base backups, with the recovery window
shpyrd pg backup db --project shop                                 # one now, before something risky
shpyrd pg restore db --as db-restored --to 2026-09-25T16:58:02Z --project shop
```

These commands speak the workspace API: signed in with `shpyrd login`, they need no kubeconfig. `pg info` and the dashboard show the state (`on, daily at 02:00 UTC, kept 7d, last …, recoverable from …`). A restore never touches the source: it creates a **new** database recovered to the moment you name (RFC 3339, UTC; the latest possible when omitted), any second inside the window, with its own credentials; when it is ready, `shpyrd attach db-restored` and detach the old one. Restores are refused before the earliest recoverable point, in the future and onto the database itself; the new database takes the source's size unless `--size` names another Postgres size. `shpyrd pg backups disable` stops archiving; existing backups stay restorable until the database is deleted, when its bucket goes with it.

Where the backups live depends on the platform's object store ([Object storage](/docs/extensions#object-storage)). On shpyrd cloud, and on a cluster you run that was installed with `--object-storage-credentials-file`, they go through the platform's S3 gateway into a bucket at the cloud provider: they outlive the cluster, and the operator can reach them there during a recovery. On a cluster without the gateway (a local one), they live in the cluster's own store and go with the cluster; `pg_dump` what must survive it. Either way, a [platform backup](/docs/backups) restores the database's definition on a new cluster, not its contents.

### Sleep

A database nobody is connected to can be put to sleep: its instance stops, its volume and data stay, and the first connection wakes it. While it sleeps you pay for the volume only. Sleep is an enterprise feature (auto sleep): always on shpyrd cloud, with a license on a self-hosted install.

```shell
shpyrd pg sleep db --project shop --after 30m      # sleep after 30 min without client connections
shpyrd pg sleep db --project shop --after off      # never sleep, whatever the workspace's default
shpyrd pg sleep db --project shop --after default  # follow the workspace's default again
shpyrd pg suspend db --project shop                # stop now and stay stopped; connections are refused
shpyrd pg resume db --project shop
```

A workspace can set a default quiet period for its databases; a database with no policy of its own follows it. `off` is a policy of its own: the database stays awake even when the workspace's default changes.

How it decides: every five minutes the platform counts the database's client sessions. Any connection counts — an application's idle connection pool keeps its database awake, on purpose; a database sleeps when its app has no connection open, which is what happens when the app itself is [asleep](/docs/cli#deploying-and-running) or has no pool. The database will not sleep when the count is stale (the metrics pipeline is down), when the quiet period has not elapsed, or when the wake proxy is not running; `pg info` says which.

How it wakes: the database's address stays the same. While it sleeps, connections land on a proxy that holds them, starts the database and hands them over once PostgreSQL accepts connections — the client sees a slow connect, not an error. **Expect up to 30–40 s** for the first connection after sleep, while PostgreSQL starts; the next connections take milliseconds. An app whose first request needs its database therefore sees the app wake plus the database wake; the app's `resuming: page` mode covers that with a "waking up" page, `wait` mode may exceed HTTP client timeouts.

Only single-instance databases sleep; a database with `--instances 2` or more exists to be available. Setting or removing a policy re-releases the attached apps once (their database host changes to the platform's wake-capable address). Sleep keeps the volume; it is not a backup — see above for those.

Not there yet: connection pooling and credential rotation.

## Redis and Valkey

```shell
shpyrd redis create cache --project shop                  # Valkey 8, cache mode
shpyrd redis create queue --project shop --persistent --storage 2Gi   # append-only file on a volume
shpyrd redis create legacy --project shop --engine redis  # upstream Redis 7
shpyrd redis resize cache shared-m --project shop         # another size; the store restarts
shpyrd redis cli cache --project shop -- INFO memory
```

A store takes a size from the Redis list, the processes' names with clients of their own, and gets the smallest when it names none:

| Size | CPU | Memory | `maxmemory` | Clients |
|---|---|---|---|---|
| `shared-s` (default) | 0.5, shared | 64 MiB | 48 MiB | 100 |
| `shared-m` | 0.5, shared | 256 MiB | 192 MiB | 400 |
| `shared-l` | 1, shared | 512 MiB | 384 MiB | 1000 |
| `shared-xl` | 2, shared | 1 GiB | 768 MiB | 2000 |
| `dedicated-s` | 1 | 1 GiB | 768 MiB | 2000 |
| `dedicated-m` | 2 | 4 GiB | 3 GiB | 5000 |
| `dedicated-l` | 4 | 8 GiB | 6 GiB | 10000 |

A resize restarts the store: a cache comes back empty, a persistent store reloads its file. A store made before stores had sizes of their own and naming a size the list does not have is given the Redis size its pod runs with.

[Valkey](https://valkey.io) (BSD licensed, protocol compatible) is the default engine; `--engine redis` selects upstream Redis. A store is a single instance run by the shpyrd controller: `maxmemory` is 75% of the size's memory; a **cache** evicts with `allkeys-lru` and loses its content on restart, which is the expected behaviour of a cache; a **persistent** store keeps an append-only file on a volume and refuses writes instead of evicting when full. Persistence cannot change after creation. High availability through an operator comes later.

## Attaching

```shell
shpyrd attach db --project shop                 # DATABASE_URL, DATABASE_HOST, DATABASE_PORT, DATABASE_USER, DATABASE_PASSWORD, DATABASE_NAME
shpyrd attach cache --project shop              # REDIS_URL, REDIS_HOST, REDIS_PORT, REDIS_PASSWORD
shpyrd attach sessions --prefix SESSIONS        # SESSIONS_URL, ... when two stores of the same kind are attached
shpyrd detach db --project shop
```

Attaching adds a binding to the app and releases it (`Attach Postgres db`); the variables are read-only on the project's **Config** page and in `shpyrd secrets list`, shown with the resource providing them, and they win over a config var of the same name. If the resource is still provisioning, the app waits (phase `Pending`, "waiting for an attached resource") and releases when it is ready. Detaching removes the variables in a new release, and rollback restores the attachments a release had. The project's **Resources** page has **Attach**/**Detach** buttons and an **Add resource** menu with the same forms.

Resources live inside the project's network policy: only the project's own processes can reach them, other projects cannot.
