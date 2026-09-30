---
title: Databases and caches
description: PostgreSQL databases (CloudNativePG) and Redis-compatible stores (Valkey) as project resources, attached to apps as config vars.
---

Two extensions add data stores to projects: `postgres` runs PostgreSQL databases with the CloudNativePG operator, `redis` runs Valkey or Redis caches and queues. Attach one to the app and it appears as `DATABASE_URL` or `REDIS_URL`. {% .lead %}

```shell
shpyrd extensions enable postgres
shpyrd extensions enable redis
```

## PostgreSQL

```shell
shpyrd pg create db --project shop                        # PostgreSQL 17, 5Gi, 1 instance, default size
shpyrd pg create db --project shop --size shared-m --storage 20Gi --instances 3   # HA with replicas
shpyrd pg list --project shop
shpyrd pg psql db --project shop -- -c 'select version()'
shpyrd pg delete db --project shop --yes                  # refused while attached (or --force)
```

Each database is its own [CloudNativePG](https://cloudnative-pg.io) cluster in the project namespace: streaming replication and failover when `--instances` is 2 or 3, a `db-rw` service for the primary and `db-ro` for replicas, a database `app` owned by user `app`. The instance size sets CPU and memory, with a floor of 256 MiB because PostgreSQL does not start below it; storage grows (`shpyrd pg create` again is not needed, edit the resource) but never shrinks.

### Backups and point-in-time recovery

With the `object-storage` extension enabled ([Extensions](/docs/extensions#object-storage)), a database can be backed up continuously: WAL archiving plus a daily base backup into a bucket of the platform's store that only this database's key can open, kept for the retention period.

```shell
shpyrd pg create db --project shop --backups --retention 14d       # or later:
shpyrd pg backups enable db --project shop --retention 7d --schedule "0 3 * * *"
shpyrd pg backups list db --project shop                           # base backups, with the recovery window
shpyrd pg backup db --project shop                                 # one now, before something risky
shpyrd pg restore db --as db-restored --to 2026-09-25T16:58:02Z --project shop
```

`pg info` and the dashboard show the state (`on, daily at 02:00 UTC, kept 7d, last …, recoverable from …`). A restore never touches the source: it creates a **new** database recovered to the moment you name (RFC 3339, UTC; the latest possible when omitted), any second inside the window, with its own credentials; when it is ready, `shpyrd attach db-restored` and detach the old one. Restores are refused before the earliest recoverable point and onto the database itself. `shpyrd pg backups disable` stops archiving; existing backups stay restorable until the database is deleted, when its bucket goes with it.

Backups live in the cluster's object store and go with the cluster: a [platform backup](/docs/backups) restores the database's definition on a new cluster, not its contents. Copying the in-cluster store to the provider's bucket is the open follow-up ([RFC-0046](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0046-object-storage.md)); until then, `pg_dump` what must survive the cluster.

### Sleep

A database nobody is connected to can be put to sleep: its instance stops, its volume and data stay, and the first connection wakes it. While it sleeps you pay for the volume only.

```shell
shpyrd pg sleep db --project shop --after 30m      # sleep after 30 min without client connections
shpyrd pg sleep db --project shop --after off      # never sleep
shpyrd pg suspend db --project shop                # stop now and stay stopped; connections are refused
shpyrd pg resume db --project shop
```

How it decides: every five minutes the platform counts the database's client sessions. Any connection counts — an application's idle connection pool keeps its database awake, on purpose; a database sleeps when its app has no connection open, which is what happens when the app itself is [asleep](/docs/cli#deploying-and-running) or has no pool. The database will not sleep when the count is stale (the metrics pipeline is down), when the quiet period has not elapsed, or when the wake proxy is not running; `pg info` says which.

How it wakes: the database's address stays the same. While it sleeps, connections land on a proxy that holds them, starts the database and hands them over once PostgreSQL accepts connections — the client sees a slow connect, not an error. **Expect about 30–40 s** for the first connection after sleep on a cloud block volume (PostgreSQL start plus volume attach); the next connections take milliseconds. An app whose first request needs its database therefore sees the app wake plus the database wake; the app's `resuming: page` mode covers that with a "waking up" page, `wait` mode may exceed HTTP client timeouts.

Only single-instance databases sleep; a database with `--instances 2` or more exists to be available. Setting or removing a policy re-releases the attached apps once (their database host changes to the platform's wake-capable address). Sleep keeps the volume; it is not a backup — see above for those.

Not there yet: connection pooling and credential rotation ([RFC-0039](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0039-postgres-pooling-rotation-resize.md)).

## Redis and Valkey

```shell
shpyrd redis create cache --project shop                  # Valkey 8, cache mode
shpyrd redis create queue --project shop --persistent --storage 2Gi   # append-only file on a volume
shpyrd redis create legacy --project shop --engine redis  # upstream Redis 7
shpyrd redis cli cache --project shop -- INFO memory
```

[Valkey](https://valkey.io) (BSD licensed, protocol compatible) is the default engine; `--engine redis` selects upstream Redis. A store is a single instance run by the shpyrd controller: `maxmemory` is 75% of the size's memory; a **cache** evicts with `allkeys-lru` and loses its content on restart, which is the expected behaviour of a cache; a **persistent** store keeps an append-only file on a volume and refuses writes instead of evicting when full. Persistence cannot change after creation. High availability through an operator comes later ([RFC-0010](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0010-redis-resource.md)).

## Attaching

```shell
shpyrd attach db --project shop                 # DATABASE_URL, DATABASE_HOST, DATABASE_PORT, DATABASE_USER, DATABASE_PASSWORD, DATABASE_NAME
shpyrd attach cache --project shop              # REDIS_URL, REDIS_HOST, REDIS_PORT, REDIS_PASSWORD
shpyrd attach sessions --prefix SESSIONS        # SESSIONS_URL, ... when two stores of the same kind are attached
shpyrd detach db --project shop
```

Attaching adds a binding to the app and releases it (`Attach Postgres db`); the variables are read-only in the Config tab and in `shpyrd secrets list`, shown with the resource providing them, and they win over a config var of the same name. If the resource is still provisioning, the app waits (phase `Pending`, "waiting for an attached resource") and releases when it is ready. Detaching removes the variables in a new release, and rollback restores the attachments a release had. The dashboard's Resources card has **Attach**/**Detach** buttons and an **Add resource** menu with the same forms.

Resources live inside the project's network policy: only the project's own processes can reach them, other projects cannot.
