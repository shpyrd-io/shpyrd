# RFC-0070 Internal names: projects calling projects, with identity

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (in progress: allow lists, the edge), RFC-0036 (implemented:
internal front door)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

A project reaches another project of its workspace at a stable internal name,
`http://crm.internal`, without leaving the cluster and without knowing Kubernetes names. The
call goes through the platform's internal front door, so the callee's allow list (RFC-0033)
decides whether it is admitted, and the callee receives a signed identity saying which
project is calling — `svc:acme/expenses` — the way apps receive a person's identity today.
No token to manage on the caller's side: the platform knows who is calling from where the
call comes from.

## Motivation

- Allow lists (RFC-0033 "Network") shipped in v0.7.0: `allow: [{project: crm}]` opens the
  network between two projects. What to call is still a Kubernetes Service name
  (`web.app-acme-crm.svc.cluster.local`), which nobody building small software should see,
  and which changes with the workspace prefix.
- The callee has no idea who is calling. An internal API of `crm` cannot tell `expenses`
  from `shop`; it would need its own token scheme, which is exactly what the edge removed
  for people (RFC-0033).
- RFC-0033's "Machines" section promised service accounts, `svc:<ws>/<project>`, for this;
  RFC-0072 (app actions) and RFC-0032 (the MCP server calling apps) need the same identity
  for the platform's own calls.

### Goals

- `http://<project>.internal` (and `https://`) works from any instance of any project in
  the workspace that the callee allows; nothing else resolves.
- The callee receives `X-Shpyrd-Service: svc:<workspace>/<project>` and a JWT with
  `realm: service`, verifiable at the workspace's JWKS like the person JWTs.
- A refused call is refused at the front door with a clear body, and counted like edge
  denials.
- No code and no configuration in the caller beyond the URL.

### Non-Goals

- Service mesh, mTLS between pods, retries, circuit breaking.
- Calls across workspaces (never, per RFC-0033).
- Calls from outside the cluster as a service (a personal or OAuth token does that today).

## Proposal

- **Names.** `<project>.internal` resolves, inside the workspace's namespaces, to the
  internal front door: CoreDNS answers every `*.internal` name with the internal ingress
  controller's ClusterIP (a `template` block in the Corefile the installer manages, or a
  small DNS shim). The disambiguation across workspaces is at the front door, not in DNS:
  the internal ingress routes `crm.internal` by looking at **who is calling** — the source
  address is a pod of exactly one workspace — and serves that workspace's `crm`.
  `crm.acme.internal` is the fully qualified form, always valid.
- **Identity.** The front door's auth subrequest (`/edge/auth`, the same endpoint) receives
  the source address; the server keeps a pod-IP index (informer over pods labelled
  `shpyrd.io/project`) and maps it to `svc:<workspace>/<project>`. It checks the callee's
  allow list (the caller's project must be listed, or `platform: actions|mcp` for the
  platform's own callers) and answers 200 with `X-Shpyrd-Service`, `X-Shpyrd-Caller-
  Project`, and `Authorization: Bearer <JWT>` (`realm: service`, `sub: svc:acme/expenses`,
  `aud: crm`, five minutes), or 403.
- **Allow lists.** Unchanged in syntax; the NetworkPolicy stays as the second line (the
  front door is the gate that knows who; the policy keeps everything else out). The
  Connections card gains the internal URL to copy and the last calls seen.
- **The platform's own calls** (RFC-0032's MCP server calling an app's actions, RFC-0072)
  use the same path with `platform: mcp|actions` as the caller identity.
- **TLS.** `http://` inside the cluster; `https://<project>.internal` served with the
  platform CA (RFC-0057) for apps that insist.

## Design Details

- CoreDNS: `template IN A internal { answer "{{ .Name }} 60 IN A <internal-ingress-ip>" }`
  added by the installer (RFC-0061 already manages DNS pieces); on clusters where the
  Corefile is not ours, a `shpyrd-dns` Deployment forwarded from CoreDNS for the `internal`
  zone.
- Pod-IP → project index: the controller already lists pods; a shared informer in the
  server keeps `ip → (workspace, project)` with a short grace after deletion.
- nginx: an internal Ingress per project (`exposure: internal` machinery of RFC-0036 already
  exists) with host `<project>.<workspace>.internal`; the bare `<project>.internal` is
  rewritten by the front door from the caller's workspace (a Lua snippet or a server-side
  redirect to the qualified name; open question 2).
- Denials counted as `reason="service"` in `shpyrd_edge_denials_total`.
- Docs and the SDK snippet: "read `X-Shpyrd-Service`, or verify the JWT; a missing header
  means a person or nobody".

## Open questions

1. Pod-IP-based identity, or a token the caller must send (projected token from the
   platform)? Default: **pod IP** — it is what NetworkPolicy already trusts, and it needs
   no code in apps; the token variant can be added for callers outside the cluster.
2. How is the bare `crm.internal` disambiguated: rewrite at the front door from the source
   workspace, or require `crm.<workspace>.internal`? Default: **rewrite**, with the
   qualified name always accepted.
3. Do internal calls count against plan limits or metrics? Default: metrics yes (a
   "calls in" series), limits no.

## Implementation History

- 2026-09-27: RFC written (research; replaces the reference "RFC-0065 internal names" in
  RFC-0033, whose number went to build composition).
