# RFC-0081 Workspace private network: a WireGuard gateway per workspace

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (workspaces, allow lists, the edge), RFC-0008 (project
isolation: the NetworkPolicy per project), RFC-0035 (cloud profiles: the VCN, NSGs and
the operator VPN), RFC-0036 (front doors), RFC-0070 (internal names), RFC-0076 (stable
identifiers), RFC-0080 (two doors: console and workspace applications)

**Amends:** RFC-0036 (what "internal" means for a workspace that has a private network),
RFC-0035 (the operator VPN's reach), RFC-0063 (intranet ranges stay the operator's; a
customer's intranet is this RFC)

**Creation date:** 2026-09-30

**Last update:** 2026-09-30

---

## Summary

Enterprise customers ask for two things the cloud platform cannot give today: an intranet
of their own, where "internal" means *their* network and not shpyrd's, and a way to reach
it from their offices and laptops, with VPN clients or a site-to-site link from their
firewall. This RFC gives every workspace an optional **private network**: a WireGuard
gateway that runs as a pod *inside the workspace*, confined by the project isolation that
already exists, a virtual address block the customer's clients see instead of the VCN, and
stable names (`crm.acme.internal`, `db.crm.acme.internal`) for the workspace's projects and
databases. The customer's admin switches it on in the workspace settings; people download a
profile from their own profile page; nothing changes in the VCN, in Terraform or in NSGs.
The operator's own VPN is tightened at the same time so that it stops being "everyone's
internal network".

## Motivation

### What the network is today

Read from the repository and verified on the production cluster on 2026-09-30:

- **The operator VPN is on the VCN.** The WireGuard profile Terraform emits routes the
  whole VCN (`AllowedIPs = var.vcn_cidr`, `contrib/oci/terraform/vpn.tf`). With OKE's
  VCN-native pod networking the pod subnet `10.0.4.0/22` is inside that range, so a VPN
  client is, in principle, on the pods' network. The workers' NSG admits the VPN subnet on
  every port (`in-vpn-all`, `nsg.tf`): SSH, the kubelet and the node ports.
- **Project isolation is one NetworkPolicy per project namespace**
  (`internal/controller/isolation.go`, RFC-0008). Its boundary is "pods of other
  projects"; anything that is not a pod (the VCN, the VPN) is "the world".
- **"Internal" is one shared front door.** `ingress-nginx-internal` and its single
  private load balancer (RFC-0036) are reachable from the whole VCN and from every
  operator VPN client. Internal names (RFC-0070) go through the same door. So a project
  marked `exposure: internal` is on shpyrd's intranet, not the customer's, and the
  operator cannot hand customers a VPN into that network.
- **Pods are *not* reachable from the operator VPN.** A test from a VPN client against
  three production pod addresses (two web processes, one CloudNativePG primary) on
  several ports, plus ICMP, timed out everywhere. The refusal comes from the **pod
  subnet's NSG**, which admits only pods, workers and the control plane (`pods_rules`,
  `nsg.tf`) and has no rule for the VPN subnet; the packet never reaches Calico. What the
  VPN does reach is the API server (and through it `kubectl exec` and `port-forward`,
  authenticated and audited) and the nodes on every port.

Two conclusions drive the design. First, there are **two independent layers** protecting
pods from anything outside the cluster: the NetworkPolicy and the NSG. A gateway that
lives inside the cluster as a pod of the workspace inherits both; a gateway that lives in
the VCN as an instance (like the operator's) sits outside both and needs per-customer
NSGs and routes to be contained. Second, the product problem is not pods leaking; it is
that "internal" has one owner.

### Goals

- A workspace can switch on a private network and get: a WireGuard endpoint, a virtual
  address block, names for its projects and data stores, and profiles for its people.
- A VPN client of workspace A can reach only pods of workspace A, and only through the
  gateway: no VCN address, no pod address, no other workspace, no platform component.
- A site-to-site link from the customer's firewall is the same mechanism with a different
  peer.
- Nothing per customer in Terraform, the VCN or NSGs; a workspace's private network is
  created, changed and destroyed by the controller like a project.
- The operator VPN reaches the API endpoint, the private load balancer and SSH, and
  nothing else.
- A path from "one network load balancer per workspace" to thousands of workspaces that
  changes only the profile's `Endpoint`.

### Non-Goals

- Service mesh, mTLS between pods, overlay networks between clusters.
- Replacing the shared internal front door for the operator's back office; it stays.
- Projects reaching the customer's network transparently (by route). Reaching it *by name*
  is a second phase, sketched below.
- Dedicated node pools or subnets per workspace ("dedicated nodes"): a separate tier,
  listed under Alternatives, not needed for isolation.
- Dedicated clusters per customer (in the operator's tenancy or the customer's cloud): the
  existing profiles and Terraform already tell that story; the gap there is fleet
  management, not networking.
- Identity-aware access for HTTP without a VPN: the edge (RFC-0033) already authenticates
  people; "internal = signed-in members of the organisation" for HTTP apps is worth doing
  and is cheaper than any VPN, but it is a change to RFC-0036's exposure semantics, not
  this RFC.

## Proposal

A workspace setting, **Private network**, off by default. Switching it on makes the
controller create, in a namespace of the workspace:

- a **WireGuard gateway** (one pod), fronted by a UDP `LoadBalancer` Service on an OCI
  Network Load Balancer with a reserved address, published as `vpn-<workspace>.<domain>`;
- a **virtual block**, a `/24` allocated from a pool reserved for private networks
  (default `100.64.0.0/10`, the CGNAT range), stored with the workspace and never
  reused while it exists;
- a **names map** the controller rewrites on every reconcile of a project of that
  workspace: HTTP names for processes with a URL, a virtual address for every data store
  (PostgreSQL, Redis), both under `<project>.<workspace>.internal`.

The gateway pod carries the workspace's identity labels and a project label of its own
(`shpyrd.io/project: network`, `shpyrd.io/role: gateway`). That makes it *a project* for
the isolation policy: its egress can reach only pods of its own workspace (the existing
same-workspace egress rule), and the ingress policy of every project in the workspace
admits it (one new peer in `isolationPolicyFor`). It is **not** a platform namespace: a
namespace without a project label would be admitted by every project of every workspace.

People get profiles from their own profile page in the workspace application: a key pair
generated server side, a `.conf` and a QR code, `AllowedIPs` limited to the workspace's
`/24`, DNS pointed at the gateway. The admin sees the list of profiles under Private
network, revokes them there, and adds the site-to-site peer (the firewall's public key
and the customer's prefixes) in the same place.

A project keeps `exposure: internal | external`. In a workspace with a private network,
*internal* comes to mean "reachable at its `.internal` name through the workspace's
gateway", instead of "behind the operator's `nginx-internal`".

### User Stories

- An enterprise admin switches on Private network, downloads the gateway's public key and
  endpoint, and gives them to the network team, who add a WireGuard peer on the office
  firewall with a route for `100.64.7.0/24`. The office reaches `erp.acme.internal` and
  the data team's pgAdmin reaches `db.warehouse.acme.internal:5432`.
- A developer on a train opens WireGuard on their laptop, which they set up once from
  their profile page, and the back office at `admin.acme.internal` loads. Their other
  traffic does not go through the tunnel.
- An employee leaves; the admin revokes their profile; the peer is gone from the gateway
  within a reconcile.
- The operator, on their own VPN, reaches the API server, the console behind the private
  load balancer and SSH to a node, and can no longer reach a node port or a pod address.

### Alternatives

Everything considered, with what each one would have cost and why it is or is not the
proposal.

| | Option | Isolation comes from | Cost model | Verdict |
| --- | --- | --- | --- | --- |
| A | **Gateway as a pod of the workspace** (this RFC) | NetworkPolicy + pod subnet NSG, both existing | one small pod per workspace, ~2–3 USD/month | **proposal** |
| B | **Internal front door per workspace** (an ingress controller per workspace, ClusterIP only) | same as A; makes "internal" the customer's | one controller pod per workspace | phase 2; the gateway's HTTP proxy covers it meanwhile |
| C | **Cloud segmentation**: node pool, pod subnet and NSGs per workspace; OCI site-to-site IPsec or FastConnect into them | the cloud, not Calico | minimum nodes and load balancers per tenant; scheduling by workspace in the controller; NAT at the DRG for overlapping customer ranges | a *dedicated nodes* tier for customers whose auditors require network separation; does not by itself give VPN clients (OCI has no managed client VPN) |
| D | **Dedicated cluster** per customer (operator tenancy or customer cloud) | separate everything | a cluster per customer | already possible with the profiles; the missing piece is fleet operations |
| E | **Overlay mesh per workspace** (Tailscale, Headscale, Netbird, ZeroTier, Nebula) | still needs a peer *inside* the workspace, i.e. option A's pod with their agent | Tailscale per user (~6–18 USD/user/month, the customer's users); Headscale/Netbird free but one more control plane to run | not a replacement for A; an option *on top* of it for a customer who already lives in one of them: their node joins as a peer |
| F | **Gateway as an instance in the VCN, provisioned by Terraform from a list of customers** | per-customer NSGs and route tables, which do not exist | an instance per customer, `terraform apply` per sale | rejected: it recreates today's problem per customer, sits outside both existing layers, and is not self-service |
| G | **OCI Site-to-Site VPN (IPsec) or FastConnect into the VCN** | per-customer NSGs and routes | IPsec free per tunnel; FastConnect hundreds of USD/month per port | what enterprise network teams ask for; viable for two or three large customers as a hand-made exception, not as a product; terminates in the whole VCN |
| H | **No VPN for HTTP**: internal = signed-in members of the organisation at the edge, optionally from listed addresses | the edge (RFC-0033) | zero per workspace | worth doing regardless; removes the need for a gateway for most workspaces; leaves TCP (databases) and site-to-site to this RFC |

Why A over the rest: it is the only option that is contained by two layers that already
exist and are already verified, needs nothing per customer outside the cluster, is created
and destroyed by the controller, costs a pod, and keeps the customer's address plan and
ours out of each other's way (see *Addresses* below). C and D remain as tiers; E and G
remain as integrations for customers who bring them; H is a cheaper complement for HTTP.

## Design Details

### Objects the controller creates

```yaml
Namespace   ws-<workspace-id>-network
  labels:   shpyrd.io/workspace-id=<id>, shpyrd.io/workspace=<slug>,
            shpyrd.io/project=network, shpyrd.io/role=gateway, managed-by=shpyrd
Secret      wg-server      # the gateway's private key, generated once
Secret      wg-peers       # one entry per profile: public key, virtual address, kind (person | site)
ConfigMap   names          # the names map (below)
Deployment  gateway        # 1 replica; NET_ADMIN, /dev/net/tun; platform pool (RFC-0077)
Service     gateway        # type LoadBalancer, UDP 51820; OCI NLB; reserved address;
                           # ExternalDNS: vpn-<workspace>.<domain>
NetworkPolicy shpyrd-isolation   # the same policy every project gets
```

And one change in `isolationPolicyFor`: the ingress of every project of a workspace admits
`namespaceSelector: {shpyrd.io/workspace-id: <id>, shpyrd.io/role: gateway}`. Egress
needs nothing: a project may already send to pods of its own workspace; the ingress side
decides. The gateway's own policy is the base project policy, so it cannot reach platform
namespaces beyond DNS and the registry, nor other workspaces.

The namespace is deleted with the workspace, or when the setting is switched off; the
address block is kept while the workspace exists so that switching it back on gives the
same names and the same addresses.

### Inside the gateway pod

One image, kept in `contrib/network-gateway/`, testable alone on kind with a hand-written
`names` ConfigMap and the WireGuard app on a laptop:

- `wg0` with the server key and a peer per entry in `wg-peers`; a watcher reloads peers
  when the Secret changes (no restart, existing tunnels survive).
- A small DNS server answering `*.<workspace>.internal` from the names map; everything
  else is refused (clients keep their own resolver for the rest).
- An HTTP reverse proxy (Caddy) mapping HTTP names to the project's Service; `Host` is
  preserved so apps behind the edge keep working.
- DNAT rules for TCP names: `100.64.7.70:5432 → db-rw.<project-ns>.svc:5432`.
- `MASQUERADE` on the way out, so project pods see the gateway's pod address, a pod of
  their workspace.

Pod security: the project namespaces run the `restricted` standard in warn and audit mode
only (RFC-0043 is not enforced yet), so `NET_ADMIN` is admitted; when RFC-0043 lands, the
gateway namespace needs an exemption like the builds namespace. The Oracle Linux UEK
kernel on the nodes ships the WireGuard module.

### Addresses

The virtual block is the first stable address anything in a workspace has; pods receive
VCN addresses from a shared subnet and change on every deploy. Layout of a workspace's
`/24`, taking `100.64.7.0/24`:

| Range | Use |
| --- | --- |
| `.1` | the gateway; also the clients' DNS server |
| `.2`–`.63` | VPN clients, one address per profile |
| `.64`–`.254` | published names: one stable address per TCP service (databases, Redis); HTTP names share the gateway's address through the proxy |

Pool: `100.64.0.0/10` gives 16,384 `/24` blocks; smaller blocks (`/25`, `/26`) double and
quadruple that. Blocks are allocated only when a workspace switches the network on, so the
pool is sized by enterprise workspaces, not by all workspaces. Uniqueness across
workspaces is nearly cosmetic: each gateway is an island, no packet crosses from one to
another, and the only case where two blocks must differ is one person connected to two
workspaces at once. The rule is therefore *unique while the pool lasts; reuse across
different organisations is safe if it ever runs out*.

The customer's own ranges never matter: they see only their `/24`, and whatever they
publish to us (phase 2) comes in by name. Two customers both using all of `10.0.0.0/8`
coexist, and neither collides with the VCN. Tailscale uses `100.64.0.0/10` too, so a
customer who runs it asks for a block from an alternative pool (`10.255.0.0/16` by
default) when switching the network on.

### Names

`<name>.<project>.<workspace>.internal`, with `<name>` the process type for HTTP (`web`
omitted: `crm.acme.internal`) and the resource name for stores (`db.crm.acme.internal`).
The workspace segment keeps a profile usable across workspaces and keeps these names
distinct from RFC-0070's in-cluster `<project>.internal`, which stays as it is. The project
page shows a project's private names when the workspace has a network; a data store's page
shows host and port.

### Profiles and the site-to-site peer

A profile is a WireGuard peer: a key pair generated server side (the private key is
handed over once, in the `.conf`, and not stored), the next free address in the client
range, and a row in `wg-peers`. Per person and device, from the person's profile page;
listed and revocable by workspace admins. The emitted profile:

```
[Interface]
PrivateKey = <once>
Address    = 100.64.7.5/32
DNS        = 100.64.7.1

[Peer]
PublicKey  = <gateway>
Endpoint   = vpn-acme.<domain>:51820
AllowedIPs = 100.64.7.0/24
PersistentKeepalive = 25
```

A site-to-site peer is the same row with `kind: site`, the firewall's public key and the
prefixes the firewall announces, pasted by the admin. The gateway then routes those
prefixes into the tunnel, which is what phase 2 needs.

### Phase 2: the customer's network, by name

Pods cannot be given routes, so the customer's services enter by name too. The admin
declares `erp.corp.internal = 192.168.10.5:443`; CoreDNS gets a stub domain per workspace
forwarding `*.corp.internal` of that workspace to the gateway's DNS, which answers with the
gateway's ClusterIP; the gateway forwards into the site-to-site tunnel. Same map, same
settings page, opposite direction.

### Scale and limits

What does and does not scale, for ten thousand workspaces:

- **Addresses**: not a limit (above).
- **The gateway pod**: kernel WireGuard moves gigabits per core; 100m CPU and 32 Mi serve
  dozens of people at hundreds of Mbps; thousands of peers per interface are fine. Ten
  thousand active gateways at those requests are ~1,000 vCPU and ~320 GB, which is real
  money but only if every workspace switches it on; a gateway with no handshake for days
  can drop to near-zero requests and keep listening.
- **One load balancer and one public address per workspace does not scale.** OCI's default
  limits on network load balancers and reserved public addresses per region are in the
  dozens (raisable), and ten thousand public IPv4 addresses are not available at any
  price. This is the real ceiling of the first version, and it is fine for the first
  dozens of enterprise networks.
- **The way out is one shared endpoint with a port per workspace.** WireGuard identifies
  peers by key, not by port, so `vpn.<domain>:5xxxx` with a port per gateway serves ~60k
  workspaces per public address; the gateway stays a pod in its workspace and isolation
  is unchanged. Forwarding a port range to the right gateway: either listeners on the
  network load balancer (limited per balancer) or a small `hostNetwork` edge on one or
  two nodes doing DNAT by port to each gateway's ClusterIP, which has no such limit.
  Whether the OCI NLB can forward every port on one listener is to be checked; it would
  remove the edge. The profile contract is written so that only `Endpoint` changes when
  the operator moves from the first design to the second: profiles are re-emitted, the
  rest is the same.
- **Egress**: OCI's first 10 TB/month are free; intranet traffic is negligible.

### The operator VPN

Independent of the above and to be done first, in `contrib/oci/terraform`:

- `AllowedIPs` of the operator profile becomes the API endpoint subnet, the private load
  balancer subnet and the bastion subnet. The pod subnet leaves the route.
- `in-vpn-all` on the workers' NSG becomes `in-vpn-ssh` (TCP 22). Node ports and the
  kubelet are no longer reachable from the VPN; debugging goes through the API server.

### Where it appears (RFC-0080's two doors)

- **Workspace application, workspace settings → Private network** (admins): the switch,
  the block, the endpoint and the gateway's public key, the names map, the profiles list
  with revocation, the site-to-site peer, phase 2's declared names.
- **Workspace application, a person's profile → Connect to the private network**: emit a
  profile, download `.conf`, show the QR code.
- **Workspace application, project and resource pages**: the private names.
- **Console, cluster page**: an inventory of private networks: gateways, profiles,
  addresses, cost (RFC-0075).
- **The CLI**: `shpyrd network status`, `shpyrd network profile new`, `shpyrd network
  profile revoke`, over the API like everything else (RFC-0052).

### Enabling, disabling, observing

Off by default on every profile; nothing changes for a workspace that does not switch it
on. Switching off deletes the namespace and every peer; the block stays reserved. The
console inventory and `kubectl get ns -l shpyrd.io/role=gateway` show what is in use.
Drawbacks when on: one pod on the platform pool per workspace, one network load balancer
and one public address per workspace in the first design, and `NET_ADMIN` in one
namespace of the workspace.

## Open questions

1. **Block size**: `/24` per workspace (default) or `/25`. Default: **`/24`**; the pool
   is sized by enterprise workspaces and 16k blocks is enough for years.
2. **Default pool**: `100.64.0.0/10` (collides with Tailscale at a customer who runs it)
   or `10.255.0.0/16`. Default: **`100.64.0.0/10`**, with the alternative offered when
   the network is switched on.
3. **Name scheme**: `<project>.<workspace>.internal` (default) or `<project>.internal`
   matching RFC-0070 inside the cluster. Default: **with the workspace segment**; a
   person in two workspaces needs distinct names and the in-cluster scheme stays.
4. **First endpoint design**: a network load balancer per workspace (simplest; a
   `LoadBalancer` Service does it) or the shared endpoint with ports from day one.
   Default: **per workspace first**, the profile contract ready for the shared endpoint;
   switch past roughly twenty private networks.
5. **Who may emit profiles**: every member (default) or admins only. Default: **every
   member for themselves**, admins for anyone, with the list visible to admins.
6. **HTTP without a VPN** (alternative H): a separate RFC amending RFC-0036, or a section
   here. Default: **separate RFC**; it changes exposure semantics for every workspace.
7. **Pod Security when RFC-0043 enforces**: an exemption for the gateway namespace like
   the builds namespace, or user-space WireGuard without `NET_ADMIN` (still needs
   `/dev/net/tun`). Default: **exemption**.

## Implementation History

- 2026-09-30: findings on the production network (operator VPN on the VCN, pods refused
  by the pod subnet's NSG, shared internal front door) and this proposal. Suggested
  order: (1) tighten the operator VPN in Terraform; (2) the gateway image in `contrib/`,
  tested alone on kind; (3) the controller: setting, namespace, Deployment, Service,
  names map, the new ingress peer; (4) API, CLI and the workspace application: profiles
  and the settings page; (5) the console inventory; (6) phase 2.
