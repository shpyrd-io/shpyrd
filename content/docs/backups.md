---
title: Platform backups
description: Nightly encrypted backups of the platform's state to the provider's object storage, and the way back into a new cluster with shpyrd-ctl cluster restore.
---

On shpyrd cloud we run the platform and its backups for you: see [Getting started](/docs/getting-started). This page is for a cluster you run yourself. Every night the platform writes an encrypted archive of its state to a bucket in your cloud provider's object storage, outside the cluster: workspaces, projects, config vars, resources, the sources apps build from, sign-in users, teams and members. Lose the cluster, build a new one, `shpyrd-ctl cluster restore`, and the projects build and start again. {% .lead %}

![The platform backups on the console's Backups page: target, schedule, last good backup, the archives and a Back up now button](/screenshots/backups-card.png)

## What is in a backup

| Included | Not included |
| --- | --- |
| Every project: its namespace, config vars, apps (build settings, processes, sizes, bindings), volumes, databases, caches, log drains, custom domains | **Data inside volumes and databases.** Volumes are recreated empty, and a restored Postgres starts empty. A database's own archive ([point-in-time recovery](/docs/databases#backups-and-point-in-time-recovery)) is separate: with the S3 gateway it is in the provider's bucket and outlives the cluster; without it, it goes with the cluster |
| The source archives apps were built from, so a restored app builds again without a redeploy | The release history: a restored app starts at v1 with its current config vars |
| Workspaces, sign-in users and connectors (`auth-local`), teams, project members | The install record is carried for reading, not applied: the new cluster's settings come from its own `cluster init` |
| The instance size catalog and global config vars | Credentials controllers regenerate: registry, bucket keys, certificates |

The archive is a `tar.gz` encrypted with [age](https://age-encryption.org) and a passphrase generated at setup. Without the passphrase a backup is noise; **print it once and keep it outside the cluster**:

```shell
shpyrd-ctl cluster backup key > shpyrd-prod-backup-key.txt
```

## Setting it up

Backups go to a bucket that outlives the cluster, so the bucket is not part of the cluster's Terraform state. A small root of its own creates it:

### Oracle Cloud

```shell
cd contrib/oci/terraform/backups
cp terraform.tfvars.example terraform.tfvars    # tenancy, region, name (same as the cluster root)
terraform init && terraform apply
```

This creates the bucket `<name>-backups` with an IAM user and a Customer Secret Key, and writes three files (git-ignored):

- `<name>-backups.env`: the key for the backup bucket, for `--backup-credentials-file`.
- `<name>-registry.env`: the bucket `<name>-registry` for images, for `--registry-credentials-file`.
- `<name>-objects.env`: the bucket `<name>-objects` behind the platform's S3 gateway, for `--object-storage-credentials-file` (see [With the S3 gateway](#with-the-s3-gateway)).

A new key takes a few minutes to become usable. Then tell the cluster root about the bucket:

```shell
cd ..
echo 'backup_bucket = "shpyrd-prod-backups"' >> terraform.tfvars
terraform apply                                  # only <name>.vars changes: the target, endpoint and region
shpyrd-ctl cluster init --context oke-shpyrd-prod --profile oci --vars-file shpyrd-prod.vars \
  --backup-credentials-file backups/shpyrd-prod-backups.env
```

### AWS

```shell
cd contrib/aws/terraform/backups
cp terraform.tfvars.example terraform.tfvars    # name, region, profile (same as the cluster root)
terraform init && terraform apply
```

This creates the bucket `<name>-backups-<account id>` (public access blocked, encrypted at rest). Then tell the cluster root about it; it grants the platform's service account access through EKS Pod Identity, no keys anywhere:

```shell
cd ..
echo 'backup_bucket = "shpyrd-prod-backups-123456789012"' >> terraform.tfvars
terraform apply                                  # the Pod Identity association and <name>.vars
shpyrd-ctl cluster init --context eks-shpyrd-prod --profile aws --vars-file shpyrd-prod.vars
```

For the S3 gateway on AWS, `contrib/aws/terraform/object-storage` creates the shared bucket and writes `<name>-objects.env`.

### With the S3 gateway

A cluster installed with `--object-storage-credentials-file` keeps its object storage in one bucket at the provider, behind the platform's S3 gateway. Without `--backup-target`, the platform backups go there too: the target is `s3://platform-backups/platform`, a bucket of the gateway. Seen from outside the cluster, with the provider's own credential, the archives are at `s3://<shared bucket>/buckets/platform-backups/platform`. An explicit `--backup-target` keeps the archives on their own bucket, straight to the provider.

On Oracle Cloud the gateway runs as a single pod: OCI's S3 API ignores `If-Match`, so one process writes the gateway's records (`SHPYRD_GATEWAY_SINGLE_WRITER=true`, `SHPYRD_GATEWAY_REPLICAS=1`, set by the `oci` profile). On AWS it runs two.

### Any S3-compatible bucket

Without Terraform, or on a local cluster, name the target yourself:

```shell
shpyrd-ctl cluster init --backup-target s3://my-bucket/shpyrd \
  --set SHPYRD_BACKUP_ENDPOINT=https://s3.example.com --set SHPYRD_BACKUP_REGION=us-east-1 \
  --backup-credentials-file creds.env             # AWS_ACCESS_KEY_ID=… and AWS_SECRET_ACCESS_KEY=…
```

`cluster init` installs the `platform-backup` component: a CronJob in `shpyrd-system` running at 03:00 UTC (`--set SHPYRD_BACKUP_SCHEDULE="0 3 * * *"`) that keeps the 14 newest archives (`--set SHPYRD_BACKUP_KEEP=14`). The job may use 1 GiB of memory (`--set SHPYRD_BACKUP_MEMORY=1Gi`); raise it when the archives outgrow it. Without a target the component is skipped.

An alert fires when a backup run fails, or when no backup has completed for a day.

## Day to day

```shell
shpyrd-ctl cluster backup                 # one now; waits and prints the archive
shpyrd-ctl cluster backups                # target, schedule, last good backup, archives, recent runs
```

```
Target:    s3://shpyrd-prod-backups/shpyrd-prod (an access key)
Endpoint:  https://ns.compat.objectstorage.sa-saopaulo-1.oraclecloud.com
Schedule:  0 3 * * * UTC, keeping 14
Last good: 6h ago

ARCHIVE                                        SIZE      CREATED
oci.example.com-20260925-030000.tar.gz.age     11.8 KiB  6h ago
oci.example.com-20260924-030000.tar.gz.age     11.6 KiB  1d ago
```

The cluster page of the dashboard shows the same card with a "Back up now" button (cluster admins). Archives are named after the platform's domain and the time (UTC).

### Disk snapshots

Until the platform takes nightly snapshots of its own, `shpyrd-ctl cluster snapshots` protects the block disks of a storage class with provider snapshots. Run it from a scheduler, once a night:

```shell
shpyrd-ctl cluster snapshots take --class oci-bv --keep 7   # every block disk of the class; keeps 7 per disk
shpyrd-ctl cluster snapshots take --class oci-bv --system   # the platform's own disks too
shpyrd-ctl cluster snapshots list
```

These are the disks on provider block storage: the platform's own, and the project disks and databases of the cloud profiles. A snapshot is consistent at the block level. A database's own backups are the real backup; the snapshot is the fallback. Project volumes on the node's own disk (`shpyrd-local`: the local profile, and disks made on a cloud profile before block storage) have no provider snapshots.

## Restoring

The restore needs three things: a cluster that runs the platform, the archive, the passphrase.

1. **Build the cluster and install the platform** as for a new one (`terraform apply`, `shpyrd-ctl cluster init --vars-file …`). The new infrastructure's addresses and zone identifiers come from its own vars file; the archive carries the old install record for reading only. Enable the same extensions (`--enable postgres` …), or their objects are skipped with a warning.
2. **Look before you apply.** `--dry-run` downloads and decrypts the newest archive (or the one you name) and lists what it holds:

   ```shell
   shpyrd-ctl cluster restore --context oke-shpyrd-prod \
     --from s3://shpyrd-prod-backups/shpyrd-prod --passphrase-file shpyrd-prod-backup-key.txt --dry-run
   ```

   On the same cluster the credentials, endpoint and region come from the cluster's own backup target. With the S3 gateway, reinstall with the same shared bucket first: the cluster then reads its archives again. To read them without a cluster, point `--from` at `s3://<shared bucket>/buckets/platform-backups/platform` with the provider's credential. On a fresh cluster give `--credentials-file backups/<name>-backups.env` (Oracle) or let `~/.aws/credentials` sign (`--aws-profile`; AWS), with `--endpoint` and `--region` where needed.
3. **Restore.** System objects (sizes, globals, users, teams, members) are created or replaced; each project is created when its namespace is absent and skipped when it exists. Sources are uploaded to the server first, then volumes, databases, caches, drains and apps are created in that order, and the controllers take it from there: builds run, instances start, certificates are issued.

   ```shell
   shpyrd-ctl cluster restore --context oke-shpyrd-prod \
     --from s3://shpyrd-prod-backups/shpyrd-prod/oci.example.com-20260925-030000.tar.gz.age \
     --passphrase-file shpyrd-prod-backup-key.txt
   ```

**One project only.** Deleted a project by mistake? `--project <slug>` restores just that one from the newest archive (repeat the flag for several); `--no-system` leaves users and teams alone:

```shell
shpyrd-ctl cluster restore --from s3://shpyrd-prod-backups/shpyrd-prod \
  --passphrase-file shpyrd-prod-backup-key.txt --project shop --no-system
```

`--overwrite` updates objects that already exist, projects included (their apps are re-applied with the archived settings). `--file <archive>` restores from a downloaded archive instead of the bucket.

## Good to know

- **The passphrase is the backup.** `shpyrd-ctl cluster backup key` prints it; the API never serves it and the dashboard never shows it. Keep it where you keep the cluster's other secrets, not in the cluster.
- **Postgres data.** Databases keep their own continuous archive for [point-in-time recovery](/docs/databases#backups-and-point-in-time-recovery); it does not travel with the platform backup. With the S3 gateway it lives in the provider's bucket and outlives the cluster; without it, take a `pg_dump` for anything that must survive the cluster.
- **Access.** The archive holds every project's config vars: whoever can read the bucket and has the passphrase reads them. The bucket is private, the key opens only that bucket, and the archive is encrypted; treat the credentials file like the passphrase.
- **Costs.** Archives are kilobytes to a few megabytes (the sources); object storage bills by the gigabyte-month, effectively nothing at 14 archives.
