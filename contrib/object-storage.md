# Shared cloud object storage

Configure the gateway once for the platform. Terraform creates one physical
bucket and its scoped provider credential; no bucket or IAM user is created
when a project enables backups. `ObjectBucket` remains the project API.

```sh
shpyrd cluster init --profile oci \
  --object-storage-credentials-file ./shpyrd-objects.env
```

For OCI, `contrib/oci/terraform/backups` produces the file. For AWS use
`contrib/aws/terraform/object-storage`. Apply these independent states before
installing the cluster, so replacing the cluster does not destroy the data.
The file contains `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
`SHPYRD_GATEWAY_BUCKET`, `SHPYRD_GATEWAY_REGION`, and optionally
`SHPYRD_GATEWAY_ENDPOINT` (required for OCI, omitted for AWS).
Keep the generated files and Terraform state private.

With `SHPYRD_GATEWAY_BUCKET` configured, the installer directs every managed
object consumer to `http://object-storage.shpyrd-system.svc:3900`:

| Consumer | Logical bucket | Provider object prefix |
| --- | --- | --- |
| Registry, build cache, registry GC | `registry` | `buckets/registry/docker/` |
| Uploaded sources | `sources` | `buckets/sources/sources/` |
| Encrypted platform archives | `platform-backups` | `buckets/platform-backups/platform/` |
| ObjectBucket / Postgres backups | ObjectBucket's bucket name | `buckets/<logical-bucket>/` |

The registry uses its S3 driver without a data PVC or mount; redirects to
the private backend are disabled. Sources use temporary staging (`emptyDir`)
while hashing uploads, then store the archive through the gateway. Their
existing 512 MiB upload limit is independent of the S3 gateway's object size.
A signed URL authorizes reading one immutable source archive: the SHA alone
is insufficient. Treat the complete URL as a credential. Builds and exports
carry that capability; restore reuploads archives and assigns fresh URLs.

Only `object-gateway-backend` holds the provider credential. Registry,
source server and backup jobs have separate logical bucket credentials.
Project Secrets contain only their own bucket credentials. A project's logical
bucket name is `shpyrd-` plus the first 56 hex characters of SHA-256 over
`<namespace>/<ObjectBucket name>`, preventing ambiguous concatenations and
names longer than S3 permits. The separate
admin listener requires the platform bearer token and is restricted by
NetworkPolicy to the platform namespace. The gateway never grants consumers
provider bucket creation, IAM, public ACLs or cross-bucket copies.

## Service and limits

`shpyrd-server object-gateway` runs two stateless replicas; no PVC or database
is required during bootstrap. VersityGW v1.8.0 handles S3 signatures,
streaming and multipart; the shpyrd backend explicitly maps supported object
operations to isolated prefixes. Unsupported operations return S3 errors.

Descriptors and random consumer secrets live under
`__shpyrd_gateway/buckets/`. Conditional writes prevent concurrent
provisioners from overwriting credentials. The backend must implement S3
`If-Match` and `If-None-Match` conditional PUT semantics. Installation and
gateway startup verify these conditions against a disposable probe object
and fail if the provider ignores them. Keep this metadata
when migrating or recovering the physical bucket. Credential reads are
uncached, so revocation applies to the next authenticated request; already
in-flight requests may finish. This costs metadata reads per request and
must be included in provider latency and request-cost estimates.

The S3 listener caps simultaneous requests at 128 per replica. Streaming
avoids buffering a complete object; aggregate resource usage still depends
on concurrent transfers. Retention scans run hourly with bounded memory,
without one provider lifecycle rule per project. Logical versioning is
explicitly rejected. Configure provider lifecycle to abort incomplete
multipart uploads after seven days (included in the AWS Terraform module).
Do not expire registry or source objects based on age: active deployments
can refer to old data. Registry GC handles registry objects.

Usage is measured by paginated object listings. The registry caches its
measurement for ten minutes; per-project reconciliation only lists that
project's prefix. The global Storage view lists all consumer data, which
can take longer on very large installations. Object count and request
volume remain separate scale tests from a large-file throughput test.

## Existing installations

Activation is explicit; old `SHPYRD_REGISTRY_*` settings alone do not switch
the platform to the gateway. The installer neither moves data nor deletes
old claims or buckets. Before activating the gateway:

1. Pause new deploys and backup jobs for the migration window.
2. Copy registry objects into `buckets/registry/`, sources into
   `buckets/sources/sources/`, and each Garage bucket's contents into
   `buckets/<logical-bucket>/` using the namespace/name hash defined above. Preserve object keys. Copy existing platform
   archives into `buckets/platform-backups/platform/`.
3. Verify counts and SHA-256 checksums by reading back the copied files.
   Keep the original buckets and volumes until restore has been checked.
4. Install with the gateway credential file. ObjectBucket reconciliation
   replaces Garage credentials with gateway-scoped credentials. The first upgrade signs existing App source URLs before starting the
   new server. New platform restores automatically assign fresh source URLs.
5. Verify image pull/push and GC, an application build, a database backup
   and restore, and a platform backup/export. Resume writes.
6. Remove orphan `registry-data`, `shpyrd-data` and `object-storage-data`
   claims only after confirming no workload references them and the migrated
   data is complete. Existing deployments are never deleted automatically.

Local profiles without a gateway keep filesystem sources/registry and
Garage. The legacy direct-S3 registry/source settings remain available for
staged migration, including `SHPYRD_SOURCES_BUCKET=` to keep sources on disk.

## Disaster recovery without the gateway

Platform backups hold project definitions, configuration, sources, accounts
and teams. Database contents and volume files require their own backups.
The gateway is storage infrastructure shared by these independent flows.

If the cluster is gone, use the provider endpoint/region and the operator's
provider credential to read the encrypted archives directly:
`s3://<physical-bucket>/buckets/platform-backups/platform`. Keep the platform
backup passphrase outside the cluster. Reinstall with the same physical
bucket to recover logical credentials, then run `shpyrd cluster restore`.
Project database backups remain under their logical prefix and can likewise
be reached by the operator during recovery. Do not give this provider
credential to a project.

## Reproducible large-file test

See `pkg/objectgateway/integration_test.go`. Against an isolated local S3
server with the test-only credentials named there:

```sh
go build -o /tmp/shpyrd-gateway-test-server ./cmd/shpyrd-server
SHPYRD_GATEWAY_TEST_ENDPOINT=http://127.0.0.1:19300 \
SHPYRD_GATEWAY_TEST_BINARY=/tmp/shpyrd-gateway-test-server \
SHPYRD_GATEWAY_TEST_LARGE=1 \
go test ./pkg/objectgateway -run TestGatewayIntegration -count=1 -v -timeout=20m
```

The test creates and cleans its own bucket, runs the gateway in a separate
process, checks cross-consumer denial, pagination, copy, multipart and key
revocation, then uploads/downloads 1.125 GiB via direct S3, gateway single PUT
and gateway multipart, followed by two simultaneous multipart transfers of
the same size. It verifies every byte with SHA-256 and samples the gateway
process's RSS independently of the client and storage backend. See the
[measured local results](object-gateway-benchmark.md) and their limitations.
