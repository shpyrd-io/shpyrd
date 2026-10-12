# Integration API surfaces

The core process serves three independent routers. The normal listener retains
workspace/console APIs. `shpyrd-internal-api` (ClusterIP port 80 → 8083) mounts
only extension `Router.Internal()` routes. `shpyrd-public-api` (ClusterIP port
80 → 8084) mounts only `Router.External()` routes. Neither integration router
mounts UI, sessions, edge authorization, source archives or administrative APIs.
Extensions must authenticate requests: network reachability is not authorization.
The Cloud extension registers Accounts and Billing on Internal only.

Public API is opt-in, in OSS, EE and Cloud. There are no provider webhook handlers
yet. GET `/healthz` reports listener liveness, not dependency readiness. Integration
logs contain a generated request ID, surface, method, route pattern, status and
latency, never headers, body or query strings. Unknown paths log `unmatched`.

## Installation configuration

```
SHPYRD_PUBLIC_API_ENABLED=true
SHPYRD_PUBLIC_API_URL=https://api.shpyrd.io
SHPYRD_INTERNAL_API_ACCOUNTS_NAMESPACE=<accounts-project-namespace>
SHPYRD_INTERNAL_API_ACCOUNTS_APP=accounts
SHPYRD_INTERNAL_API_BILLING_NAMESPACE=<billing-project-namespace>
SHPYRD_INTERNAL_API_BILLING_APP=billing
```

These are install variables (`--set` / vars file). OCI Terraform can persist them
in `extra_vars`. No separate load balancer is needed. Without an explicit public
URL the name is `api.<SHPYRD_DOMAIN>`. The external ingress class is used regardless
of console exposure. Configure DNS/CNAME for the chosen name to the external load
balancer; TLS and Host routing explicitly use that name. Certificates follow the
Auth pattern: the platform issuer inside its DNS zone, default cluster issuer
outside it (typically HTTP-01). The workspace reconciler updates the public
Ingress/certificate and removes the Ingress when disabled; the listener is also
disabled. The ClusterIP Service and certificate may remain.

Internal NetworkPolicy requires BOTH the configured namespace and app pod label.
Defaults admit no consumer. Set the app to the actual `shpyrd.io/app` label.
Accounts web and worker processes can share the app label. Only operators should
control the trusted project namespaces. NetworkPolicy enforcement requires a
supporting CNI; it is not a replacement for the Accounts/Billing keys.

## Migration (coordinate releases; do not change a live caller first)

1. Release Core infrastructure and listeners. Configure trusted namespaces/apps,
   apply manifests, and verify the new Services and policies before Cloud cutover.
2. Build Cloud against that Core version. Cloud moves `/api/accounts/*` and
   `/api/billing/*` off the general listener. Coordinate its rollout with caller
   configuration; a brief maintenance window avoids requests to the old route.
3. Accounts: set `ACCOUNTS_CLOUD_URL=http://shpyrd-internal-api.shpyrd-system.svc`.
   Billing: set `SHPYRD_API_URL` to that same URL. Keep both keys unchanged.
   Do not change Billing's deployment CLI `SHPYRD_URL`, user links or OAuth URLs.
4. From the actual Accounts pod, execute `scripts/check-cloud.mjs`. Verify valid
   HTTP 200 and latency. From Billing, verify a read-only workspace lookup and
   the next sync. Never use creation/suspension as a connectivity probe.
5. Verify unrelated project pods cannot reach 8083; external ingress can reach
   8084 but not 8083. Verify internal routes return 404 on public/general routers
   even with valid keys. Invalid keys on internal routes must return 401.
6. Enable the public API, configure DNS, verify TLS and `/healthz`. Console, token,
   edge, Accounts and Billing paths on that hostname must return 404.

No existing production configuration is changed by this source change. Rollback
requires restoring both the Cloud release and caller URLs. Restricting the Operator
hostname itself is a separate migration: console and CLI still use it.
