---
title: Oracle Cloud (OKE)
description: Run shpyrd on Oracle Kubernetes Engine - the network and cluster from Terraform, then one command for the platform.
---

The `oci` profile installs shpyrd on Oracle Kubernetes Engine (OKE) with a public load balancer, Let's Encrypt certificates, an in-cluster registry, network policy enforcement and, optionally, automatic DNS. The reference infrastructure lives in [`contrib/oci`](https://github.com/shpyrd-io/shpyrd/tree/main/contrib/oci) as Terraform; the platform itself is `shpyrd cluster init`. {% .lead %}

Oracle Cloud went first among the cloud profiles for cost - the free tier and cheap flexible shapes - and because it exercises the harder path: a private API endpoint, private workers, CRI-O nodes. The layout is the one shpyrd's own development cluster runs on ([RFC-0035](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0035-cloud-profiles.md)).

## What you get

| | |
| --- | --- |
| Network | a VCN (`10.0.0.0/16`) with private subnets for the Kubernetes API endpoint, the workers and the pods (VCN-native pod networking), a public and a private load balancer subnet, a Bastion subnet; internet, NAT and service gateways; network security groups with the rules OKE needs |
| Cluster | OKE, Basic (free control plane) or Enhanced, private API endpoint reached over the VPN (or the OCI Bastion service), two node pools of flexible shapes: a fixed `platform` pool and an autoscaled `apps` pool ([Node pools](#node-pools)) |
| Access | a WireGuard instance in a public subnet, keys and profile from Terraform: the way to the private API endpoint, the private front door and the nodes |
| Front doors | a public OCI flexible load balancer on a **reserved address** (survives cluster rebuilds); a private one for projects marked internal ([Domains and exposure](/docs/domains)) |
| Certificates | Let's Encrypt; with a DNS provider, one wildcard certificate for every project hostname |
| Registry | the in-cluster registry with TLS from the platform CA (no OCIR account needed; OCIR stays one flag away) |
| Isolation | Calico in policy-only mode, because OKE's VCN-native CNI does not enforce `NetworkPolicy` on its own |
| DNS | optional: a public zone in OCI DNS managed by ExternalDNS, records for every host, delegated from your registrar once |

## Prerequisites

- An OCI tenancy and the [`oci` CLI](https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm) configured (`~/.oci/config`).
- [Terraform](https://developer.hashicorp.com/terraform/install) 1.5+ or [OpenTofu](https://opentofu.org), `kubectl`, an ssh key pair.
- A domain (or a subdomain of one) for the platform, for example `oci.example.com`. Its records can live in OCI DNS (recommended: shpyrd then manages them) or anywhere you can create a wildcard record.
- The `shpyrd` CLI ([Installation](/docs/installation)).

## 1. Network and cluster

```shell
git clone https://github.com/shpyrd-io/shpyrd
cd shpyrd/contrib/oci/terraform
cp terraform.tfvars.example terraform.tfvars
```

Fill in `terraform.tfvars`:

```hcl
tenancy_ocid = "ocid1.tenancy.oc1..aaaa"
region       = "sa-saopaulo-1"
name         = "shpyrd-prod"
vpn          = true                  # WireGuard instance + profile: the way to kubectl and the private front door

cluster_type = "BASIC_CLUSTER"       # ENHANCED_CLUSTER for workload identity (per-cluster fee)
node_shape   = "VM.Standard.E5.Flex" # or VM.Standard.A1.Flex (Always Free, arm64) where the region has capacity
node_ocpus   = 2                     # the platform pool: fixed size, carries the platform and every database
node_memory_gb = 12
node_count   = 2

apps_min_count = 1                   # the apps pool: autoscaled, carries processes, builds and one-off runs
apps_max_count = 3                   # 0 = no apps pool (single-pool cluster)
apps_node_ocpus = 1                  # smaller nodes: the autoscaler scales in finer steps
apps_node_memory_gb = 8

ssh_public_key_path = "~/.ssh/id_ed25519.pub"

dns_zone = "oci.example.com"   # public zone in OCI DNS; "" for none
dns_auth = "key"               # key (any cluster), workload (enhanced clusters), none
```

```shell
terraform init
terraform apply          # about 15 minutes
```

`terraform output` prints what the next steps need: the reserved load balancer address, the DNS zone's name servers, the private load balancer subnet, the DNS automation user and key, and the full `shpyrd cluster init` command (`terraform output next_steps`).

{% callout title="Basic or Enhanced?" %}
A **Basic** cluster has a free control plane; the DNS automation then uses an IAM user with an API key that Terraform creates (`dns_auth = "key"`). An **Enhanced** cluster costs about $0.10 per hour and adds workload identity: the cluster's service accounts get their permissions directly and no key exists anywhere (`dns_auth = "workload"`). Basic upgrades to Enhanced in place.
{% /callout %}

## 2. Connect the VPN and reach the cluster

The API endpoint is private. With `vpn = true` (the default) Terraform created a WireGuard instance and wrote `contrib/oci/terraform/<name>-vpn.conf`: import it in the [WireGuard app](https://www.wireguard.com/install/) (Import tunnel(s) from file) and activate it. The tunnel is split: only the VCN range (`10.0.0.0/16`) goes through it. Connected, you reach the API endpoint, the private front door (`--platform-exposure internal`, internal projects) and the nodes.

```shell
cd ..                    # contrib/oci
./kubeconfig.sh          # writes the kubectl context oke-<name> (private endpoint)
kubectl --context oke-shpyrd-prod get nodes
```

The profile is a credential; keep it with the Terraform state (git-ignored) and rotate it by tainting `wireguard_asymmetric_key.client`. The instance is a `VM.Standard.E5.Flex` with 1 OCPU and 2 GB (about three cents an hour); the Always Free micro shape is too small for Oracle Linux 9's package manager.

{% callout title="Without the VPN" %}
`vpn = false` keeps the OCI Bastion service as the way in: `./tunnel.sh &` opens a port-forwarding session and an ssh tunnel on `127.0.0.1:6443` (sessions live three hours; run it again when kubectl stops answering), and `kubeconfig.sh` points the context at it with TLS still verified against the endpoint's own address.
{% /callout %}

## 3. Delegate the zone

With `dns_zone` set, Terraform created the zone in OCI DNS and the wildcard record pointing at the reserved address. Delegate it once from the parent zone at your registrar, with the name servers from `terraform output dns_zone_nameservers`:

```
oci  NS  ns1.p201.dns.oraclecloud.net
oci  NS  ns2.p201.dns.oraclecloud.net
oci  NS  ns3.p201.dns.oraclecloud.net
oci  NS  ns4.p201.dns.oraclecloud.net
```

From then on nothing in that zone is touched by hand: ExternalDNS publishes a record for every platform and project hostname. Without a zone in OCI DNS, create `*.oci.example.com  A  <reserved address>` wherever the domain lives; `cluster init` prints the record and waits for it.

## 4. Install the platform

Terraform wrote every value the platform needs from the infrastructure into `contrib/oci/terraform/<name>.vars` — you never copy an OCID by hand; only the DNS user's key stays a separate file, because it is a secret. The command `terraform output next_steps` printed:

```shell
shpyrd cluster init --context oke-shpyrd-prod --profile oci --vars-file contrib/oci/terraform/shpyrd-prod.vars \
  --set SHPYRD_ACME_EMAIL=you@example.com --dns-key-file contrib/oci/terraform/shpyrd-prod-dns.pem --enable auth-local
```

What the file carries, and where each value comes from:

| Value | Meaning | Source |
| --- | --- | --- |
| `SHPYRD_DOMAIN` | the platform's domain | `dns_zone` |
| `SHPYRD_LB_IP` | the reserved address of the public load balancer | `reserved_public_ip` |
| `SHPYRD_INTERNAL_LB_SUBNET` | the private load balancer subnet for internal front doors | the `lb_private` subnet |
| `SHPYRD_FSS_MOUNT_TARGET`, `SHPYRD_FSS_AD` | File Storage behind shared volumes | `shared_storage` |
| `SHPYRD_DNS_*` | OCI DNS automation: provider, compartment, tenancy, region, user, key or workload identity | the zone and the DNS user |
| `SHPYRD_APPS_POOL`, `SHPYRD_PLATFORM_POOL` | the node label values the controller schedules by ([Node pools](#node-pools)) | the two node pools |
| `SHPYRD_NODE_POOL_ID`, `SHPYRD_NODE_MIN_COUNT`, `SHPYRD_NODE_MAX_COUNT` | the pool the cluster autoscaler manages and its bounds | the `apps` pool, `apps_min_count`, `apps_max_count` |

Flags and `--set` win over the file (`--platform-exposure internal`, `--set SHPYRD_REGISTRY_SIZE=100Gi`, `--internal-lb-subnet` for another subnet). The file wins over what an earlier run recorded: when Terraform changes the infrastructure, run `terraform apply` and then `cluster init` with the same `--vars-file`, and the cluster follows.

What happens, in order:

| Level | Components |
| --- | --- |
| rc0 | Prometheus Operator CRDs |
| rc1 | Calico (policy only), cert-manager, the registry credential |
| rc2 | Let's Encrypt issuers, the platform CA (generated in the cluster) and trust bundle, ingress-nginx (public load balancer on the reserved address) and, with a subnet, the internal one, the registry and the node trust for it, ExternalDNS and the OCI DNS-01 solver |
| rc3 | kpack with the Paketo builder (pushed to the in-cluster registry), kube-prometheus-stack, the wildcard certificate, the control-plane database |
| rc4 | the shpyrd server |

The installer waits for the load balancer address, for `shpyrd.<domain>` to resolve on public resolvers, and for the certificates. Twenty minutes on a fresh cluster, most of it downloads and Let's Encrypt. The summary at the end:

```
  Dashboard:  https://shpyrd.oci.example.com
  Grafana:    https://grafana.oci.example.com
  Registry:   in-cluster at 10.96.0.50:5000 (TLS from the platform CA, credential in Secret shpyrd-registry)
  External LB:   147.15.59.84 (ExternalDNS: *.oci.example.com)
  Internal LB:   10.0.10.179 (ExternalDNS: per host, exposure:internal)
```

Everything you passed is recorded in the cluster: later runs (`brew upgrade shpyrd && shpyrd cluster init --context oke-shpyrd-prod --profile oci`) need no flags, and the key never leaves the Secrets the installer wrote.

## 5. First sign-in

A fresh cluster has one credential: the **admin token**, a Secret the installer generated. The CLI already uses it through your kubeconfig; for the dashboard, open it signed in through a one-time ticket, or copy the token into the sign-in page:

```shell
shpyrd cluster dashboard --context oke-shpyrd-prod    # opens https://shpyrd.oci.example.com signed in
shpyrd cluster token --context oke-shpyrd-prod        # prints the token for the "Admin token" field
```

The token is a shared, full-rights credential meant for bootstrap and automation. Give people their own accounts instead (`--enable auth-local` above installed the sign-in service), then put yourself in a platform-admin team - the moment the first team exists, roles are enforced and anyone without one sees nothing:

```shell
shpyrd users add you@example.com --name "You" --context oke-shpyrd-prod        # prompts for a password
shpyrd teams create platform --platform-role platform-admin --member you@example.com --context oke-shpyrd-prod
```

Sign in at `https://shpyrd.oci.example.com` with that email and password. When every administrator has an account, switch the token off: `shpyrd cluster token --disable` (the CLI keeps working through your kubeconfig; `--enable` turns it back on). Company sign-in (Okta, any OpenID Connect issuer, GitHub, Google) and teams mapped to identity provider groups are in [Extensions and sign-in](/docs/extensions) and [Teams, roles and security](/docs/access).

## 6. First project

```shell
shpyrd projects create shop --context oke-shpyrd-prod
shpyrd deploy --project shop --context oke-shpyrd-prod
```

Certificates are publicly trusted, so `https://shop.oci.example.com` opens with no warnings - from its first request when a DNS provider issues the wildcard. From here everything works as on the local profile: [Deploying](/docs/deploying), [Databases and caches](/docs/databases), [Domains and exposure](/docs/domains).

## Node pools

A cloud cluster has two kinds of workload, and they scale differently. The platform's own components and every database are stateful: evicting them is a restart a customer notices, and their volumes attach to whatever node the pod lands on. Application processes, builds and one-off runs can always be moved. So the profile gives them separate node pools ([RFC-0077](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0077-node-pools.md)):

| Pool | Size | Carries |
| --- | --- | --- |
| `platform` | fixed, `node_count` | ingress, Prometheus, the control-plane database, KEDA, cert-manager, kpack, the operators, the wake proxy, the autoscaler; every Postgres and Redis resource |
| `apps` | autoscaled, `apps_min_count` to `apps_max_count` | web and worker processes, release and one-off Jobs, build pods |

The nodes are told apart by the label `shpyrd.io/pool` (OKE node pools have no taints); the controller puts a node selector for the apps pool on everything it schedules for an app and pins databases and stores to the platform pool. The cluster autoscaler manages the apps pool alone: a node joins when a process has no room, and leaves when it has been under half used for ten minutes — which, with [sleep](/docs/cli) (`shpyrd sleep`) putting idle apps to zero, actually happens. The platform pool never changes size on its own.

`apps_min_count = 1` keeps one warm node so a sleeping app wakes in seconds; `0` lets the pool empty when every app sleeps, and the first request then also waits for a node (about two minutes). `apps_max_count = 0` (the default in Terraform) means no apps pool: a single-pool cluster as before.

**Adding the pool to a running cluster.** Set the `apps_*` variables, `terraform apply`, then `cluster init` with the vars file right away — the file now names the apps pool as the one the autoscaler manages, and the controller adds the selector to every app; the rolling update moves the processes as the autoscaler adds nodes for them, old instances serving until the new ones are ready. Do not leave a long gap between the two commands: until `cluster init` runs, the autoscaler still manages the platform pool and, ten minutes after the new node gives it room, would drain one of the old nodes. Once the apps have moved, lower `node_count` to what the platform and the databases need.

In the autoscaler's log, `node pool not found for instance` for a platform node is expected: it only knows the apps pool.

## Costs

At the defaults, on the pay-as-you-go price list: two `VM.Standard.E5.Flex` platform nodes (2 OCPU, 12 GB) about $0.10 per hour each, plus one to three apps nodes (1 OCPU, 8 GB) about $0.04 per hour each while they exist; a flexible load balancer at 10 Mbps; the registry's 50 GB block volume; Enhanced clusters add about $0.10 per hour. The Bastion service, the VCN, the reserved addresses, the DNS zone, Calico and the Always Free VPN instance (`VM.Standard.E2.1.Micro`) are free; DNS queries are billed per million. `VM.Standard.A1.Flex` (Ampere, arm64) is Always Free up to 4 OCPUs and 24 GB when the region has capacity - everything shpyrd runs is multi-arch.

## Good to know

- **Network policy.** OKE with VCN-native pod networking accepts `NetworkPolicy` objects without enforcing them. The profile installs Calico in policy-only mode (Oracle's supported path) so projects are isolated from each other and from the instance metadata service; `--set SHPYRD_NETWORK_POLICY=none` skips it on a cluster that already enforces policies. `cluster init` warns on any cluster where it finds no policy engine.
- **Block volumes start at 50 GB.** A `shpyrd volumes create data --size 1Gi` is created at 50Gi and the command says so; the registry's volume is 50 GB for that reason. Snapshots (`shpyrd volumes snapshot`) are block volume backups. See [Volumes on Oracle Cloud](/docs/resources#volumes-on-oracle-cloud).
- **Shared volumes need File Storage.** Set `shared_storage = true` in `terraform.tfvars` and pass the two `--set SHPYRD_FSS_MOUNT_TARGET=… --set SHPYRD_FSS_AD=…` values `next_steps` prints to `shpyrd cluster init`. It needs the File Storage service limits `Mount Target Count` and `File System Count` above zero in the availability domain (Console: Governance > Limits, Quotas and Usage > File Storage); some tenancies start at 0 and must request an increase. The mount target is free; file systems bill by the space used.
- **CRI-O.** OKE nodes run CRI-O, which refuses unqualified image names such as `redis:7`; shpyrd's own images are fully qualified, and so should yours be in a Dockerfile.
- **OCIR instead of the in-cluster registry.** `--registry-host <region>.ocir.io/<tenancy-namespace> --registry-user <namespace>/<user> --registry-token-file <file>` uses OCIR; the registry components are then skipped.
- **Platform backups.** `contrib/oci/terraform/backups` creates a bucket that outlives the cluster and a key that opens only it; `backup_bucket` in the cluster root puts the target in the vars file, `--backup-credentials-file backups/<name>-backups.env` hands the key to `cluster init`. Nightly archives, `shpyrd cluster backup` now, `shpyrd cluster restore` on a new cluster: [Platform backups](/docs/backups).
- **Upgrading.** `brew upgrade shpyrd` then `shpyrd cluster init` on the context. The recorded settings carry over; the CLI installs the server image of its own version.

## Tear down

What the platform created in the cloud through Kubernetes must go first, or it outlives the cluster. `shpyrd cluster destroy` on the context does that in order and waits for the cloud to confirm each step: every project with its data, the load balancers, then the remaining disks (registry, server data, monitoring). Then Terraform removes the cluster, the network and the DNS zone.

```shell
shpyrd cluster destroy --context oke-shpyrd-prod
cd contrib/oci/terraform && terraform destroy
kubectl config delete-context oke-shpyrd-prod
```

The DNS zone's delegation at the registrar is the one thing left to remove by hand. The backup bucket (`contrib/oci/terraform/backups`) is untouched: it is there to restore from; `terraform destroy` in that directory removes it when the archives are no longer wanted.
