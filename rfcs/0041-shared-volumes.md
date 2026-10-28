# RFC-0041 Shared volumes (ReadWriteMany)

**Status:** implemented (2026-10-28; the first text proposed an NFS server of the platform's own, dropped — see Implementation History)

**Owner:** unassigned

**Depends on:** RFC-0006 (implemented), RFC-0060 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-28

## Summary

A shared volume (`shpyrd volumes create --shared`, ReadWriteMany) is a file system
several instances and processes mount at once. Each profile names its class in
`SHPYRD_STORAGE_CLASS_SHARED`: OCI File Storage (`shpyrd-fss`, the `storage-fss`
component) on `oci`, EFS (`shpyrd-efs`, `storage-efs`) on `aws`, and the node-local
class on `local`, where several processes share one node. Where no shared class is set
up, the API refuses `--shared` in words instead of leaving a claim Pending.

## Motivation

`shpyrd volumes create --shared` produced a claim that stayed Pending on kind because
nothing provisioned ReadWriteMany, and on a cloud profile a shared volume could land on a
node-local directory. A disk (ReadWriteOnce) pins its process to one instance and a
Recreate rollout; files that web and worker, or two instances of web, read and write as
files need a class that mounts from several nodes.

### Goals

- Shared volumes bind and mount from several instances and processes.
- A clear refusal instead of a Pending claim when no shared class is set up.
- A shared volume on a cloud profile is never a node-local directory.

### Non-Goals

- Making SQLite safe on a shared volume (documented as unsafe).
- A shared class of the platform's own (an NFS server in the cluster): dropped, see
  Implementation History.

## Design (as shipped)

- Profile variable `SHPYRD_STORAGE_CLASS_SHARED`: `shpyrd-fss` on `oci`, `shpyrd-efs`
  on `aws`, empty on `local`, where `ProjectSharedStorageClass` returns the project's
  node-local class (several processes on one node).
- `oci`: the `storage-fss` component renders StorageClass `shpyrd-fss`
  (`fss.csi.oraclecloud.com`, `Immediate`, `Delete`): one file system per volume behind
  the mount target `SHPYRD_FSS_MOUNT_TARGET` in `SHPYRD_FSS_AD`. Exports squash every
  client to uid/gid 0, because the CSI driver applies no fsGroup to NFS and app processes
  never run as root; only kubelets reach the mount target, and a kubelet mounts an export
  only for a pod whose namespace holds the claim. In-transit encryption is off (it needs
  `oci-fss-utils` on the nodes and port 2051). `contrib/oci/terraform` creates the mount
  target, its security group and the policy with `shared_storage = true` and writes both
  values into the vars file; `cluster init` skips the component while the mount target is
  empty (RFC-0060).
- `aws`: the `storage-efs` component maps `shpyrd-efs` to the EFS CSI driver, one access
  point per volume in the file system `SHPYRD_EFS_ID`, owned by uid/gid 1000.
- Refusals: the API answers "Shared folders are not available on this cloud yet. For files
  several instances share, use a bucket." with an operator hint; the Volume controller
  refuses a claim whose class does not exist with "shared volumes are not offered on this
  platform yet: its operator sets them up".
- Size: File Storage and EFS do not enforce the size; the API says so at creation and
  billing follows use.
- Docs: the SQLite warning stays; shared volumes are zone-bound like disks.

## Implementation History

- 2026-09-22: RFC written, proposing a `storage-rwx` extension: `csi-driver-nfs` plus a
  Ganesha NFS server backed by a RWO volume on the local profile, EFS on AWS.
- 2026-09-25: RFC-0060 shipped the provider classes (`shpyrd-fss`, `shpyrd-efs`), the
  Terraform for the mount target and the refusals. The proof-of-concept tenancy's
  mount-target limit was 0, so `shared_storage` defaults to off.
- 2026-10-22 (the storage plan of 2026-10-10, shpyrd-cloud
  `docs/plans/2026-10-10-storage-durability-and-scale.md`): the NFS server idea dropped;
  RWX is the provider's file system; on kind the node-local class serves several processes
  on one node; a shared volume on a cloud profile never again becomes a node-local
  directory; the API refusal in words; buckets for apps (RFC-0046) cover files several
  instances share meanwhile.
- 2026-10-28: marked implemented. Turning the class on in production (Terraform, the
  roll, the end-to-end verification, the backup decision for shared folders) is
  operations, tracked in shpyrd-io/shpyrd-cloud#62.
