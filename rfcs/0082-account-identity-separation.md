# RFC-0082 Account identity separation

**Status:** implemented (production; audited 2026-10-12)

**Last update:** 2026-10-12

**Replaces:** RFC-0012 local password flow, RFC-0014 local credential lifecycle,
RFC-0053 Core MFA/passkeys; amends RFC-0007, RFC-0008, RFC-0033, RFC-0058 and RFC-0080.

## Responsibilities

Accounts owns central identity UX (Clerk), commercial contacts and products.
Accounts calls Billing directly. Registering an account creates no workspace:
`/signup/cloud` is the public Cloud landing, and `/products/workspaces/new`
creates additional workspaces from the account center.

A workspace owns admission, memberships, teams, project permissions, invitations,
CLI/MCP grants and revocation in every edition. Accounts product/contact access
does not grant workspace or console permissions. Console administration has its
own principals and allowlist.

| Edition | Authentication | Authorization |
| --- | --- | --- |
| OSS | Operator-configured Dex static credentials, stable userID | Workspace |
| EE | Customer IdP through Dex | Workspace; company admission policy |
| Cloud | shpyrd Accounts, customer SSO, or both | Workspace |
| Console | Separately configured console providers / operator credentials | Console allowlist |

With company-only methods enabled, central Accounts cannot enter that workspace.
Removing/disabling a provider also removes permissions from existing sessions and
personal/OAuth token owners. Operator kubeconfig/admin credentials remain
break-glass access. A single offered provider redirects without a chooser flash;
errors and explicit alternatives remain visible. Provider-specific launchers use
`/api/auth/login/<provider-id>?next=/`.

## Identity and provisioning

Authorization references an internal principal ID. External identities are unique
by workspace, realm, trusted issuer, connector and upstream subject. Email is
mutable contact data, never a login-time permission key. Same-email identities
remain distinct, including in the console. Administrative email selection is only
a convenience when unambiguous; APIs also accept explicit principal IDs.

Dex's signed `federated_claims`, requested with `federated:id`, supplies connector
and upstream user ID. The RP validates the broker token, issuer, audience, nonce
and expected connector. Upstream issuer comes from trusted connector config.
Accounts provisioning creates the bound principal and owner membership atomically,
using the server-verified Clerk issuer/subject. A browser-supplied user ID is never
the source of ownership.

`creation_fingerprint` is SHA-256 of length-prefixed immutable creation fields:
request ID, contact ID, issuer, subject, email, slug and name. It correlates retries;
the service credential still authenticates Accounts. A retry with different input
conflicts, and a successful retry never restores a revoked owner. Status requests
use `X-Shpyrd-Creation-Fingerprint`.

Invitations do not create credentials. The recipient signs in using an allowed
IdP with verified email. Acceptance consumes the invitation and applies its
membership/team in one transaction; concurrent acceptance cannot bind two people.
The workspace sends its invitation email. Password/MFA/recovery messages belong
to the IdP. The old Cloud signup verification and workspace password/reset/signup
ticket endpoints, local account CRUD and their email templates are removed.

## Production state (2026-10-12)

Accounts, Core and Cloud are deployed with this separation. Central Accounts and
customer SSO login have been exercised in production. Legacy users are bound
explicitly as they return; this is not an automatic migration of every old user.

Accounts provides email verification, password/passwordless login, recovery,
TOTP with recovery codes, and passkey management through Clerk. Enabled methods
follow the IdP configuration. Workspace invitations and CLI/MCP device consent
and revocation stay in the workspace; trusting Accounts does not suppress those
authorization screens. The remaining Dex auth/error-page visual work is separate.

Accounts products have a random 12-digit integer ID, presented as `XXXX-XXXX-XXXX`.
The Cloud workspace UUID is the external resource ID, not a slug or the product ID.
Accounts calls Billing directly with product identity; workspace authorization
remains independent of commercial records.

Core/Cloud v0.9.92 isolates integrations on the internal API listener. Accounts
uses `http://shpyrd-internal-api.shpyrd-system.svc` with its service credential;
Billing uses that service with its own credential. NetworkPolicy admits the
configured caller namespace and app label. Accounts provisioning/status/slug and
Billing integration routes are not exposed on the general or public API router.
`https://api.shpyrd.io` serves the separate public API listener; future provider
webhooks are not implemented. See [Integration API](../docs/integration-api.md).
Authenticated requests from Accounts and Billing pods were verified in production,
and external requests to these integration routes returned 404.

## Changes since the audit

- 2026-10-23 (core v1.4.0, #245; Cloud v1.4.1, shpyrd-cloud#54; Accounts #7): Accounts
  lists every workspace an account can enter, not only those it created. Core's
  `Store.MembershipsOf` finds the memberships of the active workspace principal bound to an
  issuer, connector and subject (email never matches; suspended people and unbound legacy
  principals hold none); Cloud answers `POST /api/accounts/memberships` on the internal
  API with the identity in the body. Products shows those workspaces beside the product
  contacts (member-only rows open, but cannot be set up or retried; membership grants no
  product-contact rights), and after sign-in an account with any product or membership goes
  to Products: Cloud signup only when the lookup succeeded and found nothing, an error when
  it failed.
- 2026-10-26 (core v1.5.1, #257; Accounts #9): a refused account can switch to another.
  Signing in again through the same provider brought back the same Accounts session, and a
  suspended workspace looped (refusal, billing gate, sign-in, refusal). A refusal now stays
  on the sign-in page with its reason and keeps `next`; a login method may name a
  switch-account page (`ext.OIDCProvider.SwitchAccountURL`, from the Dex connector's
  annotation `shpyrd.io/switch-account-url`), offered after a refusal as "Sign in with
  another …". Accounts' `/switch-account` ends the session, opens the sign-in and returns to
  the workspace's `/api/auth/login/<provider>`, accepting only `https://<label>.<workspaces
  domain>`; a custom domain lands on Products. Production's `shpyrd-accounts` connector
  carries `https://accounts.shpyrd.io/switch-account`. The workspace's own session is
  independent of Accounts': switching in Accounts does not sign anyone out of a workspace.

## Rollout and legacy-user runbook

1. Back up the control-plane database and Accounts product database. Keep the
   previous images and deployment configuration. Retain operator kubeconfig or
   admin-token access during migration.
2. Configure trusted providers and all console/workspace Dex callback URLs.
   Cloud's central connector ID is `shpyrd-accounts`; its upstream issuer must
   equal the issuer used by Accounts provisioning. Configure the Accounts↔Cloud
   service key on both servers. Configure Billing in Accounts independently.
3. Deploy matching Core/Cloud and Accounts versions together: migration 25 renames
   the provisioning column; migration 26 adds principal references and clears
   existing browser sessions. Existing IDs and permission references are retained.
   Pre-login permission recipients become unbound legacy principals.
4. Explicitly bind each legacy principal to a verified upstream subject, using
   operator credentials and trusted IdP inventory. Do this before asking users to
   log in, otherwise the new external identity deliberately creates another ID.
   Never derive this mapping automatically from email. Workspace APIs:
   - `GET /api/workspace/people` returns IDs.
   - `POST /api/workspace/principals/:id/bind` with
     `{"provider":"shpyrd-accounts","subject":"user_..."}`.
   Console APIs use `/api/cluster/principals` and
   `/api/cluster/principals/:id/bind`; the principal must belong to the console
   realm. Where the legacy console row has no console principal, sign in once
   through the console IdP, then explicitly authorize its ID with
   `shpyrd-ctl console-users add PRINCIPAL_ID`. Remove the old unbound entry
   with the console-user removal API (email is accepted only when unambiguous).
   Binding is audited; already-bound identities cannot be silently replaced.
5. For OSS, keep `enablePasswordDB: true` and configure Dex `staticPasswords`
   with bcrypt hashes and stable, never-recycled `userID` values. No password
   recovery or credential-management API is exposed by shpyrd. The extension
   registers redirect-based workspace and console login.
   For Cloud/customer SSO-only, set `SHPYRD_DEX_LOCAL_LOGIN=false` in the server
   environment and `enablePasswordDB: false` in Dex. Retain the Dex broker.
   Old Password CRs are not deleted automatically; disable local auth first and
   archive/remove them separately after verification.
6. Verify central-only, company-only and mixed login, account creation without a
   product, workspace provisioning/retry, invitations, CLI/MCP revocation and
   console isolation. Verify readiness and generated UI assets.

Migration 26 cannot safely be reversed with a down migration: restoring
email-based keys would merge identities. Roll back matching binaries/configuration
and restore the pre-migration database backup during maintenance.

The optional Kubernetes RBAC mirror now emits `shpyrd:principal:<internal-id>`
usernames, never email or raw upstream groups. Operators using direct Kubernetes
OIDC must adapt their authenticator mapping; workspace API/CLI access and
operator kubeconfig do not require this mapping.

## Validation

- Tests cover issuer/subject isolation, email changes/reuse, explicit binding,
  stable permission references, suspension, same-email console principals,
  concurrent invitation acceptance, old backup import, owner provisioning and
  idempotent retries without privilege restoration.
- Removed auth/signup routes have regression checks ensuring they return 404.
- Existing IdP removal and company-only policy have session/token-owner policy
  regression coverage. CLI/MCP tests exercise workspace-owned authorization.
- PostgreSQL migration/store tests use an isolated disposable database.
- Full core suite additionally revealed and corrected backup and RBAC mirror
  fixtures. Accounts: 46 tests passed, 2 opt-in integration tests skipped.
- Workspace and console production builds completed.

## Local integration verification

The matching Core, Cloud and Accounts changes were deployed on a disposable Kind
cluster with schema 26 clean. Real Accounts provisioning, retry and isolation
checks passed. The browser round trip through Dex and Accounts preserved the
explicitly bound owner's principal and permissions without creating a duplicate.
Disabling consent on the trusted development OAuth application was independently
verified and a fresh authorization completed without an approval screen.
Workspace CLI/MCP consent remains independent. That local verification did not change production; the later production rollout is recorded above.
