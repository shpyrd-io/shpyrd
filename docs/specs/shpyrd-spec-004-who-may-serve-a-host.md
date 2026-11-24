---
id: shpyrd-spec-004
type: spec
title: Who may serve a host
status: active
date: 2026-11-24
related: [shpyrd-glo-001]
governs: [pkg/tenancy/owner.go, pkg/api/domain_ownership.go, pkg/api/domains.go, pkg/api/edge.go, internal/controller/domains.go]
---

## Purpose

A project lists the custom domains it wants to serve in `spec.domains` of
its App. This spec says which hosts a project may serve, where that rule is
applied, and what happens to a host that is refused. It is used by the API
that writes Apps, by the index that maps a request's host to an app, and by
the controller that builds Ingress rules and Certificates.

That the DNS of a host points at the front door says nothing about who may
serve it: a workspace can point a wildcard record at the platform, and
every name under it then resolves here.

## Behavior

### The rule

A host is owned by a workspace when the host itself, or any name above it
at any depth, is one of:

- the workspace's address;
- a custom domain the workspace has verified;
- an address the workspace moved from, while its redirect has not expired;
- under the tenancy layout, the address of one of the workspace's apps.

The most specific name decides: a subdomain one workspace verified wins
over its parent verified by another. An unverified custom domain and an
expired moved address own nothing.

The answer for a host is one of: owned by a workspace, owned by nobody, or
not known. A lookup that fails is "not known"; it is never read as "owned
by nobody". The rule is `tenancy.HostOwner`; `tenancy.Owners` is the same
rule with answers remembered for a short time (10 seconds by default), and
it never remembers a failure.

Which door a request enters is a different question, answered by the
resolver, and is not changed by this rule.

A project may serve a host when the host is a valid hostname, is not a name
of the platform, is not owned by another workspace, and is not held by
another project.

### Writes through the API

Every write of `spec.domains` through the API passes one validation, in the
place where Apps are written: when a project is created, before anything is
created, and in the function every later change goes through. A handler
does not validate domains itself. `spec.domains` is assigned only by the
helpers of `pkg/api/domain_ownership.go`; a test fails when another file of
`pkg/api` assigns it.

When a write changes the list, the list is stored lower-cased, without a
trailing dot and without duplicates, and each host the write adds is
judged:

| Host | Answer |
| ---- | ------ |
| Not a valid hostname, or a wildcard | 400 |
| A name of the platform, or a name under the platform's domains | 400 |
| The workspace's own address | 400 |
| Already the project's hostname | 400 |
| Owned by another workspace | 409, `<host> belongs to another workspace` |
| Held by another project of the same workspace | 409, naming that project |
| Held by a project of another workspace | 409, `<host> is already in use by another project` |
| Ownership or holders could not be read | 502, and nothing is written |

"Held" means the host is in the `spec.domains` of another App or in a rule
of another project's Ingress. A refusal never names another workspace or a
project of another workspace.

A write that adds one refused host is refused whole. A write may add at
most 100 hosts.

Only the hosts a write adds are judged. A write that leaves the same set of
hosts keeps the stored list as it is, whatever its capitals or order. A
host the App already holds stays in its spec through later writes even when
the rule would now refuse it; the controller then does not serve it, and it
can be removed through the domains endpoint.

### The index of hosts

The index that maps a host to an app never lets a custom domain listed by a
project replace a host that belongs to a workspace, and leaves out a listed
domain that another workspace owns. An App whose workspace is not known is
treated as the default workspace's. A listed name that is not a valid
hostname is left out. When ownership or the list of workspaces could not be
read, the host is left out and that index is not kept for later requests.

### The controller

The controller applies the rule to every App, whatever wrote its spec. At
the start of a reconcile it takes the answer for each host of the spec
once, and every step of that reconcile uses that one set.

A host is refused when it is owned by another workspace, is a name of the
platform, or is not a valid hostname. A refused host:

- has no Ingress rule, no TLS entry and no Certificate, and what exists for
  it, its TLS Secret included, is removed in one reconcile;
- is never the redirect target, also when `spec.redirectTo` names it;
- has an entry in `status.domains` with the certificate state `failed`, no
  DNS lookup, and a message that says why and names nobody.

A refused host does not stop the App's other hosts from being served and
updated.

When the answer for a host is not known (the lookup failed, or the App's
workspace is not known), the controller changes nothing in the routing of
that App: Ingress hosts, TLS entries, Certificates and `status.domains`
stay as they were, nothing is added and nothing is removed, and the
reconcile is tried again within 30 seconds. Routing that ends for another
reason in that reconcile, such as a suspended workspace or a removed web
process, still ends.

An App that has domains is reconciled again within 5 minutes even when
nothing changed, so a host under a domain that another workspace verifies
later stops being served without any change to the App.

## Interfaces

- `tenancy.HostOwner(ctx, store, layout, host)`: the owning workspace, nil
  when nobody owns the host, or an error. `tenancy.ErrInvalidHost` for a
  name that is not a hostname; `tenancy.ValidHost` is that check alone.
- `tenancy.Owners`: `Owner`, `Check(workspaceID, host)`, `Platform(host)`,
  `Forget()`. `Check` answers a `tenancy.Availability`: `HostUnknown` (the
  zero value), `HostFree`, `HostTaken`, `HostPlatform`, `HostInvalid`.
- `controller.Config.HostAvailability`: the lookup the controller asks. The
  server sets it when it builds the reconciler, and a test fails when it
  does not. A controller built without it applies no rule.
- `POST /api/projects`, `POST /api/projects/:slug/deploy` and
  `POST /api/projects/:slug/domains` answer as the table above.
- `status.domains` of an App: the entry of a refused host.

## Limits and non-goals

- A host nobody owns can be listed by any project. Proof that the project's
  owner controls the domain is not asked for.
- Two writes that claim one unowned host at the same moment can both be
  accepted.
- An App written without the API (with the Kubernetes API directly) is not
  validated when written; the controller's rule is what applies to it.
- Removing a Certificate and its Secret does not revoke a certificate that
  was already issued; it stays valid until it expires.
- The refusal is shown through the existing `failed` certificate state.
  There is no state of its own for it.
