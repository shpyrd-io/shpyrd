---
title: Extensions and sign-in
description: Optional capabilities switched on per cluster, and how people sign in to the dashboard with their own accounts.
---

On shpyrd cloud the platform is run for you and every capability on this page is on: see [Getting started](/docs/getting-started). This page is for the operator of a cluster you run yourself. shpyrd keeps its core small and adds optional capabilities as **extensions**, Go packages compiled into the binaries and switched on per cluster. The first one gives the dashboard real user accounts. {% .lead %}

## Extensions

```shell
shpyrd-ctl extensions list
shpyrd-ctl extensions enable auth-local
shpyrd-ctl extensions disable auth-local --yes
```

```
NAME            STATUS    COMPONENT                        DESCRIPTION
auth-local      enabled   dex                              Sign in with email and password: a bundled Dex issuer stores local accounts (shpyrd users add)
logs-agent      enabled   logs-agent                       Vector log agent: collects container logs from all project pods, labels them project/process/instance, feeds drains (RFC-0022a)
postgres        enabled   cnpg, barman-cloud, pg-gateway   PostgreSQL databases for projects (CloudNativePG), attached to apps as DATABASE_URL (shpyrd pg create)
redis           enabled   -                                Redis-compatible caches and queues for projects (Valkey or Redis), attached to apps as REDIS_URL (shpyrd redis create)
object-storage  enabled   object-storage                   S3-compatible storage through the cloud gateway or local Garage with a key per consumer: the backing store for Postgres backups and platform backups (RFC-0046)
mail            enabled   -                                Send email from the platform: invitations and notifications over SMTP (shpyrd-ctl mail set)
sleep           disabled  keda, keda-http                  Scale web processes to zero after a quiet period and wake them on the first request: KEDA and its HTTP add-on (shpyrd sleep)
opencost        disabled  opencost                         Infrastructure cost allocation via OpenCost (RFC-0075)
```

`shpyrd extensions` answers too when a kubeconfig is at hand. Enabling installs the extension's component with the same runlevel installer as the base stack (ordering, readiness waits, install record) and restarts the server with the extension; the choice is recorded in the cluster, so `shpyrd-ctl cluster init` and `shpyrd-ctl cluster status` keep it. Disabling removes the component and is refused while resources of the extension still exist. The console's **Components** page lists every extension with its state.

Extensions contribute an installer component, resource types with controllers, API routes, CLI commands and login providers through a few small Go interfaces. Databases (Postgres, Redis), object storage and the log agent are extensions. Shared volumes are not: they come from the profile.

## Enterprise features

Some features are the enterprise's: sign-in through GitHub, Google, Microsoft or any OpenID Connect provider, auto sleep, costs and cost drains, and the MCP server. Their source is public, in `ee/` of the repository, under the shpyrd Enterprise License; they are built into the released binaries and switch on with a license: `shpyrd-ctl license set <file>`, shown on the console's Settings page. A license switches them all on until its expiry date, and off on that day. One bought from shpyrd renews online: a week before it expires the cluster sends it back, with the last 30 days of what it used and cost (summed, nothing per project), and installs the next one; the console's Settings › License shows the last renewal and opens your billing account. Without one the platform runs with email and password sign-in, and without sleep, costs or MCP. On shpyrd cloud they are always on. The costs feature reads its numbers from the `opencost` extension above.

## Object storage

`shpyrd-ctl extensions enable object-storage` gives the platform an S3-compatible store. It is the working store for what needs durable objects — Postgres backups and, on the gateway, the uploaded sources and (unless they are given buckets of their own) the platform backups and the registry — not a bucket service for applications (that comes as a resource type later). It takes one of two forms:

- **The S3 gateway**, on shpyrd cloud and on a cluster you run that was installed with `--object-storage-credentials-file`. One bucket at the cloud provider holds everything; the gateway in front of it (`object-gateway` in `shpyrd-system`) gives each consumer a bucket of its own inside it. The Terraform roots write the credential file: `contrib/oci/terraform/backups` (`<name>-objects.env`) and `contrib/aws/terraform/object-storage`. The data lives outside the cluster and survives it. On Oracle Cloud the gateway runs as a single pod: OCI's S3 API ignores `If-Match`, so one process writes the gateway's records (`SHPYRD_GATEWAY_SINGLE_WRITER=true`, `SHPYRD_GATEWAY_REPLICAS=1`, set by the `oci` profile). On AWS it runs two.
- **Garage in the cluster**, without the gateway (a local cluster): [Garage](https://garagehq.deuxfleurs.fr), one node on the profile's block storage class, `SHPYRD_OBJECT_STORAGE_SIZE`: 20Gi locally and on AWS, 50Gi on Oracle Cloud. Its data goes with the cluster.

Every consumer gets a bucket **and a key that opens only that bucket**: an `ObjectBucket` resource in its namespace produces a bucket and a Secret `<name>-object-storage` next to it (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_ENDPOINT_URL`, `BUCKET`). The bucket is `shpyrd-<namespace>-<name>` on Garage, and `shpyrd-` plus a hash of the namespace and name on the gateway. A key from one project cannot list or read another project's bucket. Optional retention (`retentionDays`) expires old objects; `deletionPolicy: Retain` keeps the bucket's contents when the resource goes.

The console's **Storage** page shows the store and every bucket with its size and object count; `shpyrd-ctl object-storage list` prints the same. The store speaks plain HTTP inside the cluster; it is never exposed outside it. More on the gateway, and on moving an existing cluster to it, is in `contrib/object-storage.md` in the repository.

## Email

The `mail` extension gives the platform one SMTP sender, used for [invitations](/docs/access#inviting-people) and, later, notifications. It has no component of its own: the settings live in the Secret `shpyrd-mail` of the system namespace, written by the operator and read again every thirty seconds, so a change needs no restart.

```shell
shpyrd-ctl extensions enable mail
shpyrd-ctl mail set --host smtp.example.com --user postmaster@example.com \
    --password @/path/to/password --from "shpyrd <noreply@example.com>"
shpyrd-ctl mail status
shpyrd-ctl mail test you@example.com
```

STARTTLS on port 587 is the default; `--tls` speaks TLS from the first byte (port 465); `--plain` is for a relay on a private network only — credentials are never sent unencrypted anywhere else. PLAIN and LOGIN authentication are supported. The test message is sent by the server from inside the cluster, so it proves the settings, the network path and the sender address at once; the console's **Email** page shows the status and sends the same test. Deliveries to one address are rate limited (five in ten minutes), and every test and failure is in the audit trail. Without a sender, invitations show their link to whoever invites, to pass along.

Any SMTP relay works, and the two kinds differ in one thing. A commercial provider (Mailgun, SES, Postmark, Resend...) keeps a suppression list per sending domain on every plan: addresses that bounced, complained or unsubscribed are refused from then on, over SMTP as much as over its API, and the list is yours to edit in its panel. A relay of your own does not, and the platform keeps no such list either: its volume is invitations and alerts to people an administrator named, and the rate limit above is the only guard. Either way the platform learns only what the relay answers during delivery; a failure the relay discovers later is in the provider's log, not in the audit trail.

## Signing in with an account

Out of the box the dashboard is protected by the **admin token** (`shpyrd-ctl cluster token`), which is right for one developer and for automation (and can be switched off later, see [the admin token](/docs/access#the-admin-token)). With the `auth-local` extension, people get their own accounts:

```shell
shpyrd-ctl extensions enable auth-local
shpyrd-ctl users add ada@example.com --name "Ada Lovelace"   # prompts for the password
shpyrd-ctl users list
shpyrd-ctl users passwd ada@example.com
shpyrd-ctl users rm ada@example.com
```

The sign-in page then asks for email and password directly (the admin token moves behind a small link, and disappears once you disable it). A wrong password is shown in place; ten wrong passwords in a minute for one account are refused before they reach the issuer, and every attempt is in the audit trail. Signed-in users see who they are in the header and can sign out; a **Users** page lets platform admins add, reset and remove accounts (the same operations as the CLI). What each account may do is decided by [teams and roles](/docs/access).

### How it works

- The extension installs [Dex](https://dexidp.io), an OpenID Connect issuer, at `https://auth.<domain>` with a certificate from the cluster issuer. Accounts are Dex objects in the cluster (bcrypt hashes), so they survive restarts, upgrades and even disabling the extension.
- The shpyrd server is an OpenID Connect relying party. Email and password never leave shpyrd's own page: the server exchanges them with Dex server to server (the OAuth2 password grant) and verifies the resulting identity token exactly as it would after a redirect. External providers use the authorization code flow with PKCE. Either way: an HttpOnly session cookie, a CSRF token on every change, sessions that expire after 12 hours idle or 7 days, and sign-out that also ends the session at issuers that support it. A company identity provider is a Dex connector: configuration, not code (below).
- The `shpyrd` CLI signs in with the same accounts (`shpyrd login`) and speaks the workspace API. `shpyrd-ctl` goes through your kubeconfig, so cluster operations do not depend on dashboard accounts.

## Company identity providers

Anyone with an identity provider that speaks OpenID Connect (Okta, Auth0, Keycloak, Microsoft Entra, Google Workspace...) connects it to shpyrd as a connector of the bundled issuer of `auth-local`, and so do GitHub, Google and Microsoft. These are enterprise features: on with a license, always on shpyrd cloud. Every provider becomes a button on the sign-in page. Since v0.9.15 each workspace can add methods of its own on its Sign-in tab (`shpyrd sso add`), shown on that workspace's login page only — see [Your company's sign-in](/docs/access#your-company-s-sign-in). Users are the same person across providers when the email matches, and roles are granted by email or by group, so an Okta group or a GitHub team can be a shpyrd [Team](/docs/access).

### GitHub, Google, Microsoft and OpenID Connect through Dex

All four go through Dex (the `auth-local` extension must be enabled) and are configured with one command — or from the dashboard's **Workspace › Sign-in** tab — that writes a Dex connector; the button appears on the sign-in page at once.

```shell
shpyrd-ctl auth connector add github --client-id ... --client-secret "$GITHUB_CLIENT_SECRET" --org acme
shpyrd-ctl auth connector add google --client-id ... --client-secret "$GOOGLE_CLIENT_SECRET" --hosted-domain acme.com
shpyrd-ctl auth connector add microsoft --client-id ... --client-secret "$MS_CLIENT_SECRET" --tenant acme.com
shpyrd-ctl auth connector add oidc --id okta --issuer https://acme.okta.com --client-id ... --client-secret "$OKTA_CLIENT_SECRET" --label Okta
shpyrd-ctl auth connector list
shpyrd-ctl auth connector remove github
```

`--client-secret` also takes `@path` to read the secret from a file. `--realm` says whose login page shows the button: `platform` (the default: the methods every workspace offers) or `console` (the operator's own door). `--tenant` limits Microsoft sign-in to one Entra tenant (id or domain); without it any work or school account may sign in. The `oidc` connector takes any issuer (Okta, Keycloak, Auth0, Authelia) and asks for the `groups` scope, so the provider's groups map to teams.

The callback URL to register at every provider is `https://auth.<domain>/callback` (the command prints it).

**At GitHub**: Settings › Developer settings › OAuth Apps › New OAuth App, with that callback URL. `--org` limits sign-in to members of one organisation and makes its teams available as groups named `org:team-slug` (`--group acme:platform` on a Team); without it, everyone with a GitHub account may sign in and all their teams come along. GitHub must have a verified email on the account.

**At Google**: an OAuth client id of type Web application in the Google Cloud console, same callback URL; `--hosted-domain` limits sign-in to one Workspace domain. Google groups need a service account and are not configured by shpyrd.

**At Okta**: Applications › Create App Integration › *OIDC - OpenID Connect*, *Web Application*; grant type Authorization Code; the callback URL above as the sign-in redirect URI; leave DPoP off. The issuer is the org URL (`https://acme.okta.com`, not `-admin`). For groups, set a groups claim on the application (Sign On › *OpenID Connect ID Token* › Edit › Groups claim type **Filter**, `groups` **Matches regex** `.*`). Group names are matched exactly as the provider sends them (Okta sends names, Microsoft Entra sends object ids): `shpyrd teams create platform --platform-role platform-admin --group "DevOps admin"`.

{% callout title="Local cluster note" %}
The kind cluster serves `auth.127.0.0.1.nip.io` with the development CA, so run `shpyrd-ctl cluster trust-ca` once or the server will not trust the issuer. On other hosts, `shpyrd-ctl cluster init --domain` decides the hostname.
{% /callout %}

## Who may join, and company domains

Two workspace settings decide what happens when someone signs in (dashboard: **Workspace › Sign-in**):

- **Who may join** — *anyone who can sign in* (the default: whoever passes one of the methods becomes a person of the workspace, with no roles until granted), *only accounts of a claimed domain* (new people join only through a verified company domain), or *only people already listed* (an administrator invited them, gave them a workspace role, or listed their email in a team or a grant first). An [invitation](/docs/access#inviting-people) lets someone in under every policy. People who already signed in keep their access whatever the policy.
- **Company domains** — claim `acme.com` by publishing the DNS TXT record the page shows (`_shpyrd-verify.acme.com`) and pressing *Verify*. Accounts of a verified domain count as the company's people; choose a sign-in method for the domain and `@acme.com` accounts can only come through it — nobody signs in as `ceo@acme.com` with a password they made up elsewhere.

Every person who signs in belongs to the built-in **everyone** team (`shpyrd members add intranet --team everyone --role user` opens an app to the whole company), and the People tab of the Workspace page can **suspend** someone: their access ends at once, everywhere, until reactivated.
