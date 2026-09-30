---
title: AWS (EKS)
description: Run shpyrd on Amazon EKS - the network, cluster and VPN from Terraform, then one command for the platform.
---

The `aws` profile installs shpyrd on Amazon EKS with Network Load Balancers for the public and the internal front door, Let's Encrypt certificates, Route 53 automation, an in-cluster registry and network policy enforcement from the VPC CNI. The reference infrastructure lives in [`contrib/aws`](https://github.com/shpyrd-io/shpyrd/tree/main/contrib/aws) as Terraform, including an AWS Client VPN as the way into private parts of the platform; the platform itself is `shpyrd cluster init` ([RFC-0035](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0035-cloud-profiles.md)). {% .lead %}

## What you get

| | |
| --- | --- |
| Network | a VPC (`10.0.0.0/16`) with two public subnets (load balancers, one NAT gateway per zone) and two private `/19` subnets for nodes and pods (the VPC CNI gives pods VPC addresses) |
| Cluster | EKS in API authentication mode (the Terraform caller is the first administrator), a **private API endpoint** (reachable over the VPN; a public one restricted to your address is opt-in), one managed node group on Amazon Linux 2023, standard support only |
| Credentials | EKS Pod Identity: IAM roles associated with the service accounts that need AWS (the load balancer controller, the EBS and EFS CSI drivers, ExternalDNS, cert-manager). No access keys are created or stored |
| Front doors | Network Load Balancers from the AWS Load Balancer Controller with **pod targets**: an internet-facing one on two **Elastic IPs** (static addresses for allow-lists and apex A records), an internal one for projects marked internal ([Domains and exposure](/docs/domains)). DNS uses their hostnames as alias records |
| Certificates | Let's Encrypt; with the zone in Route 53, one wildcard certificate for every project hostname through cert-manager's Route 53 solver |
| Registry | the in-cluster registry with TLS from the platform CA |
| Isolation | the VPC CNI's own network policy agent enforces `NetworkPolicy` (no Calico needed) |
| Storage | EBS `gp3` (encrypted, 1 GiB minimum) with snapshots; EFS for shared volumes ([Resources](/docs/resources)) |
| DNS | optional: a public zone in Route 53 managed by ExternalDNS, delegated from your registrar once |
| Access | optional: an AWS Client VPN endpoint into the VPC, with a profile for the AWS VPN Client |

## Prerequisites

- An AWS account and the [`aws` CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) configured (a named profile or the environment).
- [Terraform](https://developer.hashicorp.com/terraform/install) 1.5+ or [OpenTofu](https://opentofu.org), `kubectl`.
- A domain (or a subdomain of one) for the platform, for example `aws.example.com`, whose zone can live in Route 53.
- The `shpyrd` CLI ([Installation](/docs/installation)); the [AWS VPN Client](https://aws.amazon.com/vpn/client-vpn-download/) for the VPN.

## 1. Network, cluster and VPN

```shell
git clone https://github.com/shpyrd-io/shpyrd
cd shpyrd/contrib/aws/terraform
cp terraform.tfvars.example terraform.tfvars
```

Fill in `terraform.tfvars`:

```hcl
profile = "default"          # aws CLI profile; "" for the environment
region  = "us-east-1"
name    = "shpyrd-prod"

kubernetes_version = "1.36"
node_instance_type = "t3a.large"
node_count         = 2
# nat_gateway_per_az = false         # one shared NAT gateway instead of one per zone

dns_zone       = "aws.example.com"   # public zone in Route 53; "" for none
vpn            = true                # Client VPN endpoint + profile: the way to kubectl
shared_storage = true                # EFS for shared volumes
# api_public_access = true           # also a public API endpoint, restricted to your address
```

```shell
terraform init
terraform apply          # about 15 minutes
```

`terraform output next_steps` prints the rest: the kubectl context script, where the VPN profile was written, the zone's name servers and the full `shpyrd cluster init` command.

## 2. Connect the VPN

The Kubernetes API is private: only the VPC and VPN clients reach it. Terraform generated a certificate authority, the server certificate (imported to ACM) and one client certificate, and wrote `contrib/aws/terraform/<name>-vpn.ovpn`. Import it in the AWS VPN Client (File > Manage Profiles > Add Profile) and connect. The tunnel is split: only the VPC range goes through it, and DNS goes to the VPC resolver so private names resolve.

Connected, you reach what the internet cannot: the API endpoint, the internal front door (projects marked `exposure: internal`, or the whole platform with `--set SHPYRD_PLATFORM_EXPOSURE=internal`). The profile is a credential; keep it with the Terraform state (it is git-ignored) and rotate it by tainting `tls_private_key.vpn_client`.

{% callout title="No VPN on that machine?" %}
`api_public_access = true` adds a public API endpoint restricted to `admin_cidrs` (your address at apply time by default). It is also the way back in if the profile is lost: Terraform talks to the AWS control plane, not to Kubernetes, so `terraform apply` restores access in two minutes.
{% /callout %}

## 3. Reach the cluster

```shell
cd ..                    # contrib/aws
./kubeconfig.sh          # writes the kubectl context eks-<name>
kubectl --context eks-shpyrd-prod get nodes
```

## 4. Delegate the zone

With `dns_zone` set, Terraform created the zone in Route 53. Delegate it once from the parent zone at your registrar, with the name servers from `terraform output dns_zone_nameservers`:

```
aws  NS  ns-1211.awsdns-23.org
aws  NS  ns-1671.awsdns-16.co.uk
aws  NS  ns-302.awsdns-37.com
aws  NS  ns-713.awsdns-25.net
```

Nothing in the zone is written by Terraform or by hand: ExternalDNS publishes `*.aws.example.com` as an alias of the external load balancer as soon as the platform is up, and one record per internal hostname pointing at the internal one.

## 5. Install the platform

Terraform wrote every value the platform needs from the infrastructure into `contrib/aws/terraform/<name>.vars` — you never copy an identifier by hand. The command `terraform output next_steps` printed:

```shell
shpyrd cluster init --context eks-shpyrd-prod --profile aws --vars-file contrib/aws/terraform/shpyrd-prod.vars \
  --set SHPYRD_ACME_EMAIL=you@example.com --enable auth-local
```

What the file carries, and where each value comes from:

| Value | Meaning | Source |
| --- | --- | --- |
| `SHPYRD_DOMAIN` | the platform's domain | `dns_zone` |
| `SHPYRD_AWS_CLUSTER`, `SHPYRD_AWS_REGION`, `SHPYRD_AWS_VPC_ID` | what the load balancer controller manages | the cluster; discovered from the cluster itself when absent |
| `SHPYRD_AWS_LB_EIPS`, `SHPYRD_LB_IP` | the public front door's Elastic IPs (allocation ids for the controller, addresses for the Domains card) | the two `aws_eip.lb` |
| `SHPYRD_EFS_ID` | the file system behind shared volumes | `shared_storage` |
| `SHPYRD_DNS_PROVIDER`, `SHPYRD_DNS_ZONE_ID`, `SHPYRD_DNS_REGION` | Route 53 automation | the hosted zone |

Flags and `--set` win over the file, so `--platform-exposure internal` (dashboard, sign-in and Grafana behind the internal load balancer, VPN only, apps public) or `--set SHPYRD_REGISTRY_SIZE=50Gi` go on the same command line. On a cluster you did not create with this Terraform, pass the values with `--set`; the three cluster facts are read from the cluster.

What happens, in order:

| Level | Components |
| --- | --- |
| rc0 | Prometheus Operator CRDs |
| rc1 | the AWS Load Balancer Controller, cert-manager (with ambient credentials for Route 53), the registry credential, the snapshot controller and the EBS snapshot class, the `gp3` and `shpyrd-efs` storage classes |
| rc2 | Let's Encrypt issuers, the platform CA and trust bundle, ingress-nginx behind an internet-facing NLB on the Elastic IPs and the internal one behind an internal NLB (both with pod targets), the registry and the node trust for it, ExternalDNS |
| rc3 | kpack with the Paketo builder, kube-prometheus-stack, the wildcard certificate, the control-plane database |
| rc4 | the shpyrd server |

The installer waits for the load balancer hostname, for `shpyrd.<domain>` to resolve on public resolvers and for the certificates. The summary at the end:

```
  Dashboard:  https://shpyrd.aws.example.com
  Grafana:    https://grafana.aws.example.com
  Registry:   in-cluster at 10.100.0.50:5000 (TLS from the platform CA, credential in Secret shpyrd-registry)
  External LB:   k8s-ingressn-ingressn-015ed2971e-ae9f48a7eeaa7d9f.elb.us-east-1.amazonaws.com (ExternalDNS: *.aws.example.com)
  Internal LB:   k8s-ingressn-ingressn-60fb9cbfd6-a7ec35b89089177e.elb.us-east-1.amazonaws.com (ExternalDNS: per host, exposure:internal)
```

Everything you passed is recorded in the cluster: later runs (`brew upgrade shpyrd && shpyrd cluster init --context eks-shpyrd-prod --profile aws`) need no flags.

## 6. First sign-in and first project

The same as on Oracle Cloud: the admin token bootstraps, then accounts and a platform-admin team ([Oracle Cloud, steps 5 and 6](/docs/oracle-cloud#5-first-sign-in)):

```shell
shpyrd cluster dashboard --context eks-shpyrd-prod
shpyrd users add you@example.com --name "You" --context eks-shpyrd-prod
shpyrd teams create platform --platform-role platform-admin --member you@example.com --context eks-shpyrd-prod
shpyrd projects create shop --context eks-shpyrd-prod
shpyrd deploy --project shop --context eks-shpyrd-prod
```

## Costs

At the defaults, on demand in us-east-1: the EKS control plane $0.10 per hour, two `t3a.large` nodes $0.15, two NAT gateways $0.09 plus data (`nat_gateway_per_az = false` halves it), two Network Load Balancers $0.045, the Client VPN association $0.10 plus $0.05 per connection; about $0.50 per hour all in. EBS `gp3` $0.08 per GB-month, EFS by the space used, the zone $0.50 per month. A development cluster is created for a working session and destroyed after it.

## Good to know

- **Addresses and hostnames.** The public front door has two static Elastic IPs (the A-record targets for a zone apex, shown on the Domains card) and a DNS name that ExternalDNS uses for alias records; the internal one has a DNS name only. Load balancers are managed by the AWS Load Balancer Controller with pod targets; an ALB is not used because it would terminate TLS with ACM certificates, which does not fit per-domain certificates from cert-manager.
- **Network policy.** The VPC CNI enforces it with its own agent; `cluster init` recognises it and installs nothing.
- **Snapshots** are EBS snapshots, crash-consistent: shpyrd runs `sync` in the instances mounting the volume before taking one, so what the application had written is in the copy.
- **Existing clusters.** The profile works on any EKS cluster that has the same add-ons and Pod Identity associations as the Terraform creates (CSI drivers, `shpyrd-system/external-dns`, `cert-manager/cert-manager`), and subnets tagged for the in-tree load balancer discovery.
- **Upgrading.** `brew upgrade shpyrd` then `shpyrd cluster init` on the context.
- **Platform backups.** `contrib/aws/terraform/backups` creates an S3 bucket that outlives the cluster; `backup_bucket` in the cluster root grants the platform's service account access through Pod Identity (no keys) and puts the target in the vars file. Nightly archives, `shpyrd cluster backup` now, `shpyrd cluster restore` on a new cluster: [Platform backups](/docs/backups).

## Tear down

```shell
shpyrd cluster destroy --context eks-shpyrd-prod   # projects and their data, load balancers, disks
cd contrib/aws/terraform && terraform destroy       # cluster, network, zone, VPN
kubectl config delete-context eks-shpyrd-prod
```

The zone's delegation at the registrar is the one thing left to remove by hand. The backup bucket (`contrib/aws/terraform/backups`) is untouched: it is there to restore from; `terraform destroy` in that directory removes it when the archives are no longer wanted.
