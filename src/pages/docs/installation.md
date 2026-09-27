---
title: Installation
description: Create a local cluster with the shpyrd base stack, or install it on an existing Kubernetes cluster.
---

Shpyrd ships as a single CLI, `shpyrd`, that installs the platform on a Kubernetes cluster: a local kind cluster it creates for you, or a cluster you already have - on [Oracle Cloud (OKE)](/docs/oracle-cloud) or [AWS (EKS)](/docs/aws), other providers as their profiles arrive. This page covers the local cluster and what every profile shares. {% .lead %}

## Requirements

- **Docker** (Docker Desktop on macOS/Windows, Docker Engine on Linux). Give it 6-8 GB of memory: the base stack idles around 3 GB and buildpack builds need headroom.
- Internet access for the first install (kind node image, Helm charts, buildpacks, ~2 GB) and for name resolution of the default `127.0.0.1.nip.io` domain.
- macOS or Linux, Intel or ARM. On Windows, use WSL 2.

{% callout type="warning" title="Docker Desktop and cgroup v1" %}
Recent Kubernetes releases refuse to run on cgroup v1. If Docker Desktop has the deprecated cgroup v1 setting enabled (`DeprecatedCgroupv1` in its settings), `shpyrd cluster create` detects it, applies a kubelet override and warns; switching Docker Desktop to cgroup v2 is recommended.
{% /callout %}

## Install the CLI

macOS, with Homebrew:

```shell
brew install shpyrd-io/tap/shpyrd
```

macOS or Linux, with the install script (downloads the latest [release](https://github.com/shpyrd-io/shpyrd/releases), verifies its SHA-256 checksum and installs into `/usr/local/bin` or `~/.local/bin`):

```shell
curl -fsSL https://shpyrd.io/install.sh | sh
```

`SHPYRD_VERSION=v0.1.0` pins a version and `SHPYRD_INSTALL_DIR=...` picks the directory. The archives and checksums are also on the release page for a manual install. Check with `shpyrd version`.

Every release also publishes the server image `ghcr.io/shpyrd-io/shpyrd-server:<version>` for `linux/amd64` and `linux/arm64`; the CLI installs the image of its own version, so CLI and server always match. Upgrading is `brew upgrade shpyrd` (or re-running the script) followed by `shpyrd cluster init`.

{% callout title="Building from source" %}
Developers build the CLI with `make cli` (Go 1.27) after cloning [shpyrd-io/shpyrd](https://github.com/shpyrd-io/shpyrd); a development build installs the latest released server image unless told otherwise with `--set SHPYRD_SERVER_IMAGE=...` (see the [contributing guide](/docs/how-to-contribute)).
{% /callout %}

## Create a local cluster

```shell
shpyrd cluster create
```

This runs [kind](https://kind.sigs.k8s.io) as a library to create a two-node cluster named `shpyrd` (one control-plane, one worker), then installs the base stack in dependency-ordered **runlevels**, waiting for each to be healthy:

| Level | Components |
| --- | --- |
| rc0 | Prometheus Operator CRDs |
| rc1 | cert-manager, the registry credential |
| rc2 | development CA `ClusterIssuer`, trust-manager, ingress-nginx (host ports 80/443), the in-cluster registry (TLS from the CA) and the node trust for it |
| rc3 | kpack with the Paketo buildpacks builder, kube-prometheus-stack + Grafana, the control-plane database (PostgreSQL) |
| rc4 | shpyrd server (API, App controller, dashboard) |

The first run takes 10-20 minutes, mostly downloads. Re-running `cluster create` or `cluster init` on an existing cluster is idempotent and takes about 30 seconds.

Options worth knowing:

```shell
shpyrd cluster create --http-port 8080 --https-port 8443   # host ports 80/443 already in use
shpyrd cluster create --domain myapps.example.test          # wildcard domain resolving to your machine
shpyrd cluster create --workers 2                          # more kind worker nodes
shpyrd cluster create --skip monitoring                    # lighter install, no Prometheus/Grafana
shpyrd cluster create --no-init                            # only the kind cluster
```

## Local names and ports

Two choices decide what your URLs look like, and `shpyrd cluster create` detects the fitting ones ([RFC-0057](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0057-local-names-and-front-door.md)):

| | Default | Alternative |
| --- | --- | --- |
| **Names** | `*.127.0.0.1.nip.io`: public DNS answers 127.0.0.1, nothing to install (needs internet for name resolution) | `--local-dns` with a domain such as `shpyrd.test`: a dnsmasq rule and a `/etc/resolver/test` file make every `*.test` name resolve to your machine, offline (macOS; one sudo prompt; dnsmasq installed with Homebrew if missing) |
| **Front door** | kind maps 80/443 to ingress-nginx; certificates from shpyrd's development CA (`cluster trust-ca`) | `--front-door caddy` when a Caddy already serves 443: kind takes high ports, shpyrd writes `~/.shpyrd/caddy/shpyrd.caddy` proxying `*.<domain>` to kind and reloads Caddy; certificates come from Caddy's CA you already trust, URLs carry no port |

When Caddy is detected the CLI asks:

```
Caddy v2.11.4 is serving port(s) 80 and 443 on this machine.
Use it as the front door (clean https URLs, certificates from Caddy's CA)? [Y/n]
```

and, unless `--domain` was given, uses `shpyrd.test` (offering to configure dnsmasq when the name does not resolve yet). The Caddyfile needs one line, `import ~/.shpyrd/caddy/*.caddy`, which the CLI offers to add to the Caddyfile of the running Caddy. The result:

```
  Dashboard:  https://shpyrd.shpyrd.test
  Names: dnsmasq (*.shpyrd.test) · Front door: Caddy on 443 -> kind :8080
```

`--yes` accepts the proposals (CI does this); `--front-door kind` refuses them. Both choices are recorded in the cluster, so `shpyrd cluster init`, `cluster status` and `cluster dashboard` keep them; `cluster destroy` removes the Caddy site and offers to remove the DNS rule if shpyrd wrote it. An existing cluster can switch: `shpyrd cluster init --domain shpyrd.test --front-door caddy` (identity providers then need the new redirect URIs).

## Trust the platform CA

Not needed behind a Caddy front door: Caddy issues the certificates from its own CA (run `caddy trust` once if your browser warns). Otherwise, certificates for `https://<project>.<domain>` are issued by a root CA generated on your machine (`~/.shpyrd/ca/rootCA.pem`) and stored in the cluster. Install it in your operating system trust store once:

```shell
shpyrd cluster trust-ca      # asks for sudo (macOS keychain / Linux ca-certificates)
```

Firefox keeps its own store: enable `security.enterprise_roots.enabled` in `about:config` or import the certificate.

Cloud clusters generate their own platform CA at install; it signs the in-cluster registry and the platform's internal endpoints, never the public hostnames (those come from Let's Encrypt). `shpyrd cluster trust-ca --context <cluster>` fetches and installs that CA when you need to talk to the registry from your machine.

## Check the installation

```shell
shpyrd cluster status
```

```
Profile: local  Version: v0.1.1  Domain: 127.0.0.1.nip.io  Updated: 2026-09-21T22:23:28Z
Names: public DNS (127.0.0.1.nip.io) · Front door: kind on 80/443

RUNLEVEL  COMPONENT        STATUS  VERSION  APPLIED
rc0       monitoring-crds  ready   32.0.0   ...
rc1       cert-manager     ready   v1.21.2  ...
rc2       ca-issuers       ready            ...
rc2       trust-manager    ready   v0.25.0  ...
rc2       ingress-nginx    ready   4.15.1   ...
rc2       registry         ready            ...
rc3       kpack            ready            ...
rc3       monitoring       ready   91.4.1   ...
rc3       control-plane-db ready            ...
rc4       shpyrd           ready            ...
```

Endpoints on the default domain:

- `https://shpyrd.127.0.0.1.nip.io` — dashboard
- `https://grafana.127.0.0.1.nip.io` — Grafana (`admin` / `shpyrd` on the local profile)
- `10.96.0.50:5000` — the in-cluster registry, inside the cluster only (`shpyrd cluster registry` shows its state)

## Sign in

The installer generated one credential, the **admin token**, stored as a Secret in the cluster. The CLI uses it through your kubeconfig without you noticing; the dashboard asks for it:

```shell
shpyrd cluster dashboard     # opens the dashboard signed in, through a one-time ticket
shpyrd cluster token         # prints the token for the "Admin token" field of the sign-in page
```

For one developer on a laptop that is all. For a team, enable accounts and make yourself the first platform admin - from the first team on, roles are enforced:

```shell
shpyrd cluster init --enable auth-local
shpyrd users add you@example.com --name "You"                                       # prompts for a password
shpyrd teams create platform --platform-role platform-admin --member you@example.com
```

Then sign in with the email and password, and switch the token off when nobody needs it (`shpyrd cluster token --disable`; `--rotate` replaces it). Details: [Extensions and sign-in](/docs/extensions), [Teams, roles and security](/docs/access).

## Install on an existing cluster

The installer works against any kubeconfig context:

```shell
shpyrd cluster init --context my-cluster --profile local --domain apps.example.test --yes
```

`--yes` is required for contexts that do not look like kind clusters. The `local` profile assumes ingress-nginx can bind host ports on a node labelled `ingress-ready=true` and that the service subnet is `10.96.0.0/16` (the registry uses the fixed ClusterIP `10.96.0.50`). For a managed Kubernetes cluster use a cloud profile: [Oracle Cloud (OKE)](/docs/oracle-cloud) or [AWS (EKS)](/docs/aws).

## Upgrading

Install the new CLI and run `shpyrd cluster init` again with the same context and profile: the recorded decisions (domain, exposure, extensions) are reused, and `--only shpyrd` limits the run to the platform's own components when nothing else changed. What an upgrade does to running apps:

- The apps keep serving throughout: the platform's server restarts, the apps do not depend on it at run time.
- A release that changes what every instance is given (a new platform variable such as `REVISION`, a new resource model) rolls every app's instances once, one at a time; a single-instance app is unavailable for the seconds its new instance takes to start.
- A release that changes what builds are made of (the buildpacks, the stack, the run image) makes kpack rebuild every buildpack app; the previous release keeps serving until the new image is ready, and a failed rebuild leaves it serving and marks the project so. v0.9.11 moved every image to a repository named after its workspace, which rebuilt every buildpack app once; the old repositories stay in the registry until a prune exists (RFC-0059).
- A release with a database migration (v0.9.11: identifiers became native `uuid`) migrates at the server's first start; take a backup first (`shpyrd cluster backup`, or `pg_dump` against the `control-plane-db` pod).
- Re-applying every component (without `--only`) restarts ingress-nginx, which is a real interruption of a few seconds at the front door.

## Environment profiles

A **profile** describes the environment the base stack is built for and therefore how load balancing, DNS, TLS and the registry are provided:

| | `local` | `oci` (Oracle Cloud) | `aws` (AWS) |
| --- | --- | --- | --- |
| Load balancer | kind host ports 80/443, or your Caddy | OCI flexible load balancer on a reserved address; a private one for internal projects | Network Load Balancers with pod targets (AWS Load Balancer Controller): an internet-facing one on Elastic IPs, an internal one for internal projects |
| DNS | `*.127.0.0.1.nip.io` or dnsmasq (`*.shpyrd.test`) | a wildcard record you create, or a zone in OCI DNS managed by ExternalDNS | a zone in Route 53 managed by ExternalDNS (alias records) |
| TLS | development CA issued by cert-manager | Let's Encrypt (one wildcard with a DNS provider); the platform CA for the registry | Let's Encrypt (one wildcard through the Route 53 solver); the platform CA for the registry |
| Registry | in-cluster, TLS from the CA | in-cluster, TLS from the CA; OCIR with `--registry-host` | in-cluster, TLS from the CA |
| Isolation | kindnet enforces `NetworkPolicy` | Calico in policy-only mode | the VPC CNI's network policy agent |
| Storage | kind's local path | Block Volume (50 GB minimum), File Storage for shared volumes | EBS `gp3`, EFS for shared volumes |
| Access | this machine | WireGuard instance (profile from Terraform), or the Bastion tunnel | AWS Client VPN (profile from Terraform) |

The dashboard's cluster page shows the installed profile, and the install record keeps every choice so `cluster init` re-runs need no flags.

## Export the manifests

For GitOps tooling (Flux, Argo CD) the same embedded manifests can be rendered to disk instead of applied:

```shell
shpyrd cluster export -o ./gitops --domain apps.example.test
```

## Remove everything

```shell
shpyrd cluster destroy     # deletes the kind cluster
```

Projects, images and configuration live inside the cluster and disappear with it; the development CA under `~/.shpyrd/ca` is kept so the next cluster is trusted immediately.
