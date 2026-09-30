---
title: Resources
description: A project holds several resources - the app, volumes and, soon, databases and caches - and attaching one to the app turns it into config vars.
---

A project is a namespace with resources in it: the app, volumes, PostgreSQL databases and Redis-compatible stores. `shpyrd projects info` and the project page list them all with their status and what uses them. {% .lead %}

```
Resources:
  App     hello-docker  Running  https://hello-docker.127.0.0.1.nip.io
  Volume  data          Bound    1Gi single-instance, mounted by hello-docker/web
```

Every resource is a Kubernetes object in the project's namespace (`kubectl -n app-hello-docker get apps,volumes.shpyrd.io`), so nothing is hidden from the tools you already use.

## Volumes

A volume is a persistent disk of the project, backed by a PersistentVolumeClaim that the `Volume` resource owns. Deploys, scaling and crashes never touch it; only `shpyrd volumes delete` and the project's destruction remove it.

```shell
shpyrd volumes create data --size 5Gi              # single-instance (ReadWriteOnce), the default
shpyrd volumes create assets --size 20Gi --shared  # shared (ReadWriteMany)
shpyrd volumes list
shpyrd volumes resize data --size 10Gi
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

### Single-instance and shared volumes

Kubernetes access modes decide who can mount a disk; shpyrd enforces them so you cannot end up with a rollout that waits forever or a corrupted database.

| | Single-instance (default) | Shared (`--shared`) |
| --- | --- | --- |
| Storage | block storage (`ReadWriteOnce`): available on every cluster | a network filesystem (`ReadWriteMany`): needs a provisioner that offers it |
| Who mounts it | one process type, running **one instance** | any number of instances and process types |
| Rollouts | stop, release the disk, start (a few seconds of downtime per deploy, no data risk) | rolling, as usual |
| Good for | SQLite, uploads, caches, tool state | assets shared by several instances |
| Not for | anything needing more than one instance | SQLite and other file-locking databases |

The rules are explained where they bite: `shpyrd scale web=3` on a process with a single-instance volume answers `web mounts single-instance volume "data" and can run 1 instance (a shared volume allows more)`, mounting one single-instance volume from two process types is refused, and the dashboard pins the instance count to one. For a database, use the Postgres resource when it lands rather than SQLite on a shared volume.

### Size and status

`shpyrd volumes list` shows the size, the mode, the status (`Pending: created; the disk is provisioned when a process mounts it` on clusters that bind lazily, then `Bound`, or `Failed` with the reason) and what mounts it. Volumes only grow: `resize` expands the claim when the storage class allows expansion and explains when it does not (kind's `standard` class does not). The access mode and storage class cannot change after creation.

Any image user can write to a mounted volume: the platform hands the disk to a group every container in the instance belongs to, so a Dockerfile `USER` or a buildpack's non-root user needs no `chown` step.

On the local profile the bytes live on the kind node, so `shpyrd cluster destroy` deletes them along with everything else. Cloud profiles keep disks independent of nodes.

### Volumes on Oracle Cloud

The `oci` profile puts volumes on Block Volume (`oci-bv`, balanced performance, expansion allowed) and knows Oracle's rules so they do not surprise you:

- **Disks start at 50 GB.** A request below that is created at 50Gi and the command says so: `Note: Oracle Cloud block volumes start at 50Gi: created at 50Gi instead of 1Gi`. The dashboard's size field says it up front. The same applies to a Postgres or Redis data volume, which then shows `50Gi (5Gi requested; provider minimum)`.
- **The disk exists once a process mounts it.** Until then the volume is `Pending` with that explanation; nothing is billed for it yet.
- **Shared volumes are File Storage file systems.** They need the mount target the Terraform in `contrib/oci` creates (`shared_storage = true`) and the two `--set` values it prints for `shpyrd cluster init`; without them a `--shared` volume is refused with those instructions. File Storage ignores the size (the file system grows as needed and bills by the space used), which the create command notes.
- **Snapshots** are block volume backups (incremental after the first), billed on the backup's size.

### Snapshots

Where the profile supports it (Oracle Cloud today; the local profile answers that snapshots are not available), a volume can be snapshotted and restored:

```shell
shpyrd volumes snapshot data --name before-migration   # a point-in-time copy of the disk
shpyrd volumes snapshots data                          # list them, newest first
shpyrd volumes restore data --from before-migration --to data-copy   # into a new volume
shpyrd volumes restore data --from before-migration --yes            # in place
shpyrd volumes snapshot rm data before-migration --yes
```

Restoring into a new volume is the safe path: the copy is created from the snapshot (at least the snapshot's size) and you mount it like any other volume. Restoring in place replaces what is on the volume now: the instances mounting it stop (`stopped while volume data is restored`), the disk is swapped for one created from the snapshot, and they start again as soon as it is ready; take a snapshot first if you may want the current contents back. Neither creates a release; both appear in the activity feed. The dashboard's Resources card offers the same from a **Snapshots** button on each volume.

Snapshots are taken at the block level and are crash-consistent: before taking one, shpyrd runs `sync` in the instances mounting the volume, so what the application had written is in the copy; a database is still better served by its own backups.

On AWS the `aws` profile puts volumes on EBS `gp3` (encrypted, 1 GiB minimum, expansion allowed), snapshots are EBS snapshots, and shared volumes are EFS access points in the file system `contrib/aws` creates, owned by the same group the platform hands block volumes to, so every process writes to them without further setup.

## Attaching resources

Attaching a resource to the app injects its connection details as config vars, Heroku style: a Postgres named `db` provides `DATABASE_URL`, `DATABASE_HOST`, ... The variables are read-only in the Config tab and `shpyrd secrets list`, shown with the resource that provides them, and take precedence over a config var of the same name. Attaching or detaching is a `config` release ("Attach Postgres db") that rollback undoes like any other. See [Databases and caches](/docs/databases) for `shpyrd pg`, `shpyrd redis`, `shpyrd attach` and `shpyrd detach`.
