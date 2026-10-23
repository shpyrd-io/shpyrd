# RFC-0041 Shared volumes (storage-rwx)

**Status:** implementable

**Owner:** unassigned

**Depends on:** RFC-0006 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-09-22

## Summary

A `storage-rwx` extension providing a ReadWriteMany StorageClass: an NFS server and CSI
driver on the local profile, EFS on AWS; shared volumes use it automatically, and
`--shared` is refused with an explanation when no RWX class exists.

## Motivation

`shpyrd volumes create --shared` produces a claim that stays Pending on kind because
nothing provisions ReadWriteMany.

### Goals

- Shared volumes bind and mount from several instances and processes.
- Clear refusal instead of a Pending claim when the extension is off.

### Non-Goals

- Making SQLite safe on a shared volume (documented as unsafe).

## Proposal

- Extension `storage-rwx`: local profile installs `csi-driver-nfs` plus a Ganesha-based NFS
  server backed by a RWO volume, StorageClass `shpyrd-shared` (RWX); AWS profile maps
  `shpyrd-shared` to the EFS CSI driver.
- The Volume controller sets `storageClassName: shpyrd-shared` for `ReadWriteMany` volumes
  when the class exists; otherwise the API and CLI refuse `--shared`: "no shared storage on
  this cluster: enable the storage-rwx extension".
- Docs: sizes and expansion behaviour of the NFS class; SQLite warning stays.

## Design Details

- Verify early that kind nodes (Docker Desktop kernel) can mount NFS; if not, the local
  fallback is `csi-driver-smb` or documenting shared volumes as cloud-only.

## Implementation History

- 2026-09-22: RFC written.
- 2026-10-22 (the storage plan of 2026-10-10 (shpyrd-cloud `docs/plans/2026-10-10-storage-durability-and-scale.md`)): shared folders (OCI File Storage, class `shpyrd-fss`) are
  built on OKE (RFC-0060) and refused in words until the tenancy's File Storage
  limit is raised: "files that several instances share belong in a bucket" is the
  answer meanwhile, and buckets for apps shipped (RFC-0046, step 5). End to end
  verification is the last item of the plan's hardening tail (step 7).
