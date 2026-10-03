# Object gateway large-file check — 2026-10-03

The integration test passed with the complete `shpyrd-server object-gateway`
binary, a separate native MinIO process, and a separate Go test client on
macOS/arm64 over loopback. MinIO used release 2025-09-07
(`07c3a429bfed`); the gateway uses VersityGW v1.8.0. This is a functional
streaming/memory check, not a cloud capacity or cost benchmark.

Each object contained **1,207,959,552 bytes (1.125 GiB)**, generated from a
deterministic xorshift stream with bounded client memory. Every download
matched the upload SHA-256:
`56ce3bab821198854a27680e03e4ff0e1fb70b669d8da031d5f3d53bf8356221`.

| Transfer | Upload | Upload MiB/s | Download | Download MiB/s |
| --- | ---: | ---: | ---: | ---: |
| Direct provider, single PUT | 6.60 s | 174.6 | 1.30 s | 886.1 |
| Gateway, single PUT | 14.81 s | 77.8 | 1.51 s | 764.0 |
| Gateway, multipart | 11.20 s | 102.9 | 1.99 s | 579.4 |
| Gateway, concurrent multipart A | 18.47 s | 62.4 | 6.48 s | 177.7 |
| Gateway, concurrent multipart B | 18.46 s | 62.4 | 5.95 s | 193.6 |

Multipart used 16 MiB parts and two client workers per upload. The two
concurrent uploads and their subsequent downloads finished in **25.02 s**,
transferring 2.25 GiB each way. Sampled gateway peak RSS was **60.9 MiB**
(100 ms samples via `ps`, excluding client and MinIO memory). The test
rejects gateway RSS above 512 MiB. Peak RSS does not establish a maximum for
all workloads, concurrency levels or runtimes.

An earlier run using the Go test subprocess as the gateway measured
10.40/12.42/18.06 s for direct/single/multipart uploads and 27.2 MiB peak
gateway RSS. The full server includes additional runtime/package overhead;
host load also varies. These single runs are not statistically stable
throughput comparisons.

The final binary was tested again after adding the conditional-write probe
and fixing descriptor reads to retrieve data and ETag in one request:

| Transfer | Upload | Upload MiB/s | Download | Download MiB/s |
| --- | ---: | ---: | ---: | ---: |
| Direct provider, single PUT | 18.67 s | 61.7 | 2.87 s | 401.2 |
| Gateway, single PUT | 20.36 s | 56.6 | 5.80 s | 198.7 |
| Gateway, multipart | 29.33 s | 39.3 | 4.80 s | 240.1 |
| Gateway, concurrent multipart A | 25.57 s | 45.1 | 6.69 s | 172.2 |
| Gateway, concurrent multipart B | 25.54 s | 45.1 | 6.90 s | 167.0 |

This run passed every checksum and finished the simultaneous transfers in
**32.47 s**, with **58.5 MiB** sampled peak gateway RSS. The substantial
variation in the direct-provider baseline confirms the limits of drawing
throughput conclusions from runs on this shared development machine.

Small-object integration checks cover authenticated bucket isolation,
cross-bucket copy denial, multipart isolation, pagination and revocation.
The final suite additionally covers encoded keys, range reads, presigned
GET, batch deletion, concurrent credential provisioning and revoked-key
reactivation. Conditional metadata-write semantics are checked on startup.
All these cases passed with the final full server binary. The concurrency
fix also passed three consecutive integration runs before the final large
transfer test.

Run instructions are in [the storage runbook](object-storage.md). No AWS or
OCI deployment was used. Provider latency, request charges, network egress,
replica failure during an upload, and hundreds of thousands of consumers
still require environment-specific testing. Every authenticated request
reads credential metadata from the provider; that cost is particularly
relevant for many small objects. Global usage and retention scan costs grow
with stored metadata/object counts.
