---
title: Resources
description: A project holds several resources - the app, volumes, databases and caches - and attaching one to the app turns it into config vars.
---

A project is a namespace with resources in it: the app, volumes, PostgreSQL databases and Redis-compatible stores. `shpyrd projects info` and the project page list them all with their status and what uses them. {% .lead %}

```
Resources:
  App     hello-docker  Running  https://acme-hello-docker.shpyrd.app
  Volume  data          Bound    1Gi single-instance, mounted by hello-docker/web
```

Every resource is a Kubernetes object in the project's namespace, so on a cluster you run yourself nothing is hidden from the tools you already use. The namespace is `p-<id>` (older projects: `app-<slug>`), labelled `shpyrd.io/project=<slug>`: `kubectl get apps,volumes.shpyrd.io -A -l shpyrd.io/project=hello-docker`.

## Volumes

A volume is a persistent disk of the project, backed by a PersistentVolumeClaim that the `Volume` resource owns. Deploys, scaling and crashes never touch it; only `shpyrd volumes delete` and the project's destruction remove it.

```shell
shpyrd volumes create data --size 5Gi              # single-instance, the default
shpyrd volumes create assets --size 20Gi --shared  # shared by several instances and processes
shpyrd volumes list
shpyrd volumes delete data --yes
```

Mount it from `shpyrd.yaml` and deploy:

```yaml
processes:
  web:
    volumes:
      - name: data
        path: /data
```

### Where the bytes live

On shpyrd cloud and on the Oracle Cloud and AWS profiles, a volume is a provider block volume (`oci-bv`, `gp3`): it starts at the provider's minimum (50 GB on Oracle Cloud; the size you ask for is rounded up and the answer says so), it can grow, it has nightly snapshots, and it follows its process to another node, so a node can be drained or lost without losing data. Databases and persistent stores keep their data the same way.

On the local profile (a kind cluster on your machine) volumes live on the node's own disk (the `shpyrd-local` class): no minimum, no resize, no snapshots, and the processes that mount one run on that node. Volumes made on a cloud profile before it moved to block storage are still on the node's disk; they keep their class until a cluster admin migrates them, and the platform keeps the autoscaler from removing a node that holds such data.

Backups protect the bytes either way: a database's own [backups](/docs/databases#backups-and-point-in-time-recovery), the nightly snapshots of block volumes, and the project archive a cluster admin exports from the console (a project's volumes' files and its databases, in one portable file).

### Single-instance and shared volumes

shpyrd enforces who can mount a disk, so you cannot end up with a rollout that waits forever or a corrupted database.

| | Single-instance (default) | Shared (`--shared`) |
| --- | --- | --- |
| Who mounts it | one process type, running **one instance** | any number of instances and process types |
| Rollouts | stop, release the disk, start (a few seconds of downtime per deploy, no data risk) | rolling, as usual |
| Good for | SQLite, uploads, caches, tool state | assets shared by several instances |
| Not for | anything needing more than one instance | SQLite and other file-locking databases |

The rules are explained where they bite: `shpyrd scale web=3` on a process with a single-instance volume answers `web mounts single-instance volume "data" and can run 1 instance (a shared volume allows more)`, mounting one single-instance volume from two process types is refused, and the dashboard pins the instance count to one. For a database, use the [Postgres resource](/docs/databases) rather than SQLite on a shared volume.

### Size and status

`shpyrd volumes list` shows the size, the mode, the status (`Pending: created; the disk is provisioned when a process mounts it`, then `Bound`, or `Failed` with the reason) and what mounts it. A block volume grows with `shpyrd volumes resize` (never below the provider minimum). A volume on the node's disk shares that disk's space and cannot be resized: `shpyrd volumes resize` explains that you grow the node's disk or move the project to a larger node. The access mode and storage class cannot change after creation.

Any image user can write to a mounted volume: the platform hands the disk to a group every container in the instance belongs to, so a Dockerfile `USER` or a buildpack's non-root user needs no `chown` step.

Self-hosted: on the local profile the node is the kind container, so `shpyrd cluster destroy` deletes the bytes along with everything else.

### Snapshots

Block volumes have snapshots where the profile supports them (Oracle Cloud and AWS); volumes on the node's disk have none: `shpyrd volumes snapshot` refuses them and points to the project's backups.

```shell
shpyrd volumes snapshot data --name before-migration   # a point-in-time copy of the disk
shpyrd volumes snapshots data                          # list them, newest first
shpyrd volumes restore data --from before-migration --to data-copy   # into a new volume
shpyrd volumes restore data --from before-migration --yes            # in place
shpyrd volumes snapshot rm data before-migration --yes
```

Restoring into a new volume is the safe path: the copy is created from the snapshot (at least the snapshot's size) and you mount it like any other volume. Restoring in place replaces what is on the volume now: the instances mounting it stop (`stopped while volume data is restored`), the disk is swapped for one created from the snapshot, and they start again as soon as it is ready; take a snapshot first if you may want the current contents back. Neither creates a release; both appear in the activity feed. The project's **Resources** page offers the same from a **Snapshots** button on each volume.

Snapshots are taken at the block level and are crash-consistent: before taking one, shpyrd runs `sync` in the instances mounting the volume, so what the application had written is in the copy; a database is still better served by its own backups. An operator can also snapshot every remaining provider disk at once with `shpyrd-ctl cluster snapshots` ([Platform backups](/docs/backups)).

## Attaching resources

Attaching a resource to the app injects its connection details as config vars, Heroku style: a Postgres named `db` provides `DATABASE_URL`, `DATABASE_HOST`, ... The variables are read-only on the project's **Config** page and in `shpyrd secrets list`, shown with the resource that provides them, and take precedence over a config var of the same name. Attaching or detaching is a `config` release ("Attach Postgres db") that rollback undoes like any other. See [Databases and caches](/docs/databases) for `shpyrd pg`, `shpyrd redis`, `shpyrd attach` and `shpyrd detach`.
