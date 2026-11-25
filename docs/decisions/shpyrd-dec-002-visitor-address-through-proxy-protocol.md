---
id: shpyrd-dec-002
type: decision
title: On the OCI profile the visitor's address reaches apps through PROXY protocol at the external front door, believed only from the load balancer's subnet
status: accepted
date: 2026-11-25
related: [shpyrd-glo-001]
governs: [deploy/components/ingress-nginx, deploy/components/ingress-nginx-internal, deploy/profiles/oci/ingress-nginx, deploy/profiles/oci/ingress-nginx-internal, pkg/install/proxyprotocol.go, pkg/api/proxy_protocol.go, pkg/api/internal_names.go]
---

## Context

- On the OCI profile the external front door is an ingress controller
  behind a load balancer that passes TCP through. The connection the
  controller receives comes from inside the cluster's network, so the
  address it writes in `X-Real-IP` and `X-Forwarded-For` is not the
  visitor's.
- The controller ignores `X-Forwarded-For`, `X-Real-IP` and `Forwarded` as
  sent by a client, on purpose: an app must be able to believe the address
  it reads.
- A call to an internal name is told apart by the address of its
  connection, read from `X-Real-IP` at the internal front door. Anything
  that lets a caller state that address would let it pass for another
  project.
- The server itself connects to the external front door from inside the
  cluster, for sign-in discovery and for the maintenance check.

## Decision

- With `SHPYRD_PROXY_PROTOCOL=true` on the OCI profile, the load balancer
  sends a PROXY protocol header and the external controller reads the
  visitor's address from it. Apps read it in `X-Real-IP`.
- The controller believes the header only from `SHPYRD_LB_SUBNET_CIDR`, the
  public load balancer subnet's range, and its Service uses
  `externalTrafficPolicy: Local`, so that a connection keeps the address it
  came from. The installer refuses an empty range, a range that is not a
  CIDR, and a range that covers every address.
- The internal front door never accepts PROXY protocol, on any profile.
- With the feature on, the server announces its own connections to the
  external front door with a preface that names no address, and a call to
  an internal name is refused when the cluster has no internal front door,
  instead of being sent to the external one.
- When the internal controller is left out of the install (with
  `--skip ingress-nginx-internal`, or on a profile without it), the feature
  stays off and the installer says why.
- The feature changes state only in a `cluster init` that applies both the
  external controller and the server. A run limited to some components
  (`--only`, or `--skip` of either of the two) keeps the state the external
  controller has in the cluster, read from its Service, gives the server
  that same state, and says so when it differs from what was asked.
- The variable defaults to `false`. Other profiles ignore it.

## Alternatives considered

- **A network load balancer that preserves the source address.** The
  nodes' ports would then accept connections from the internet directly,
  and the load balancer and its address would be replaced.
- **Believing `X-Forwarded-For` from the load balancer
  (`use-forwarded-headers`).** The load balancer passes TCP through and adds
  no header: the header would be the client's own.
- **Keeping `externalTrafficPolicy: Cluster` and believing the node
  subnet.** A smaller change, but a pod in the cluster could then state any
  visitor address through a node port.
- **PROXY protocol on the internal front door too.** A caller could state
  its own address, which is what tells projects apart there.
- **A second external controller and load balancer, switched by DNS, for a
  change without interruption.** It replaces the load balancer and its
  reserved address.
- **A new `X-Shpyrd-Client-IP` header.** `X-Real-IP` already arrives and
  frameworks know it.

## Consequences

- An app on the OCI profile can read the visitor's address, and a client
  cannot choose it.
- Turning the feature on or off changes how the load balancer talks to the
  controller: the external front door is interrupted during that run of
  `cluster init`.
- The controller and the server must agree: a controller that requires the
  header refuses a server that does not announce itself. That is why a
  partial run follows the cluster instead of the variable.
- With the feature on, every connection to the external controller must
  begin with a PROXY header. Anything in the cluster that connects to it
  directly must send one, as the server does; a new in-cluster caller of
  the external front door has to be written with that in mind.
- With `externalTrafficPolicy: Local`, only the nodes that run a controller
  pod receive traffic from the load balancer.
- Limits by address at the external front door now count per visitor
  instead of per cluster node.
- Worth revisiting if the load balancer comes to preserve the source
  address by itself, or if the internal front door stops identifying
  callers by the address of the connection.
