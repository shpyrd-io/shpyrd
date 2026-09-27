---
title: CLI reference
description: Every shpyrd command and its flags.
---

Two binaries, one install: `shpyrd` for people who deploy and run projects, `shpyrd-ctl` for the operator who installs and runs the platform (the cluster, extensions, accounts, backups). `shpyrd` signs in to a workspace with `shpyrd login` and needs no kubeconfig: every developer command speaks the workspace API. With a kubeconfig named on the command line (`--context`, `--kubeconfig`) it goes through the cluster instead, as `shpyrd-ctl` always does. Project commands take `--project <slug>` or read `project:` from `shpyrd.yaml` in the current directory; `-v` prints verbose output. Commands contributed by extensions explain themselves when the extension is not enabled: `pg` and `redis` (a project's resources) live in `shpyrd`, `users`, `auth` and `object-storage` (the platform's) in `shpyrd-ctl`. {% .lead %}

## Signing in

| Command | What it does |
| --- | --- |
| `shpyrd login --url <workspace> --token <token>` | Sign the CLI in to a workspace (`https://shpyrd.example.com`) and make it the **current** one; the credential is kept in `~/.shpyrd/sessions.json`. The token is a personal API token (below) or, for the operator, the admin token from `shpyrd-ctl cluster token`. Without `--token`, switches to a workspace you are already signed in to. `SHPYRD_URL` and `SHPYRD_TOKEN` work without a saved session: set them in CI. A token the workspace rejects is not saved. |
| `shpyrd use [workspace]` | List the workspaces you are signed in to (`*` marks the current one), or switch. Commands talk to the current workspace; with several sessions and none current they ask you to pick. |
| `shpyrd whoami` | Who you are at the current workspace, checked live; fails when the token expired or was revoked. |
| `shpyrd logout` | Forget the current workspace's credential (or `--url` another's). |
| `shpyrd tokens create <name>` | Create an API token for CI or another machine: `--platform-role platform-viewer\|platform-admin` or `--project <slug> --role user\|viewer\|developer\|admin`, `--expires 90d`. The value is printed once. A token never carries more than you hold at the moment it is used, and a token cannot create tokens: run this signed in as yourself (or with the admin token), or use the dashboard's Workspace → API tokens tab. |
| `shpyrd tokens list`, `tokens revoke <id>` | Your tokens with role, expiry and last use (platform admins see everyone's); revocation is immediate. |

Still cluster-only after `shpyrd login` (run them with `--context`): `run`, `pg`, `redis` and `domains`; a `shell` cannot be given a command over the API (it opens the image's shell). They say so in one line instead of failing.

## Cluster

Operator commands. They live in `shpyrd-ctl` (installed alongside `shpyrd` by Homebrew and the release archives) and also answer as `shpyrd cluster …` when a kubeconfig is available.

| Command | What it does |
| --- | --- |
| `shpyrd cluster create` | Create a kind cluster and install the base stack. `--name`, `--workers`, `--image`, `--http-port`, `--https-port`, `--domain`, `--profile`, `--skip`, `--only`, `--set SHPYRD_X=y`, `--no-init`. |
| `shpyrd cluster init` | Install or upgrade the base stack on the current context. Same profile flags; `--yes` for non-kind contexts. Cloud profiles: `--profile oci` or `--profile aws` with `--vars-file <name>.vars` (written by `contrib/*/terraform`: domain, addresses, zone, storage; flags and `--set` win over it), `--set SHPYRD_ACME_EMAIL=...`, `--platform-exposure internal`, `--dns-key-file` (OCI key), `--backup-target s3://bucket/prefix --backup-credentials-file <file>` for [platform backups](/docs/backups), `--set SHPYRD_DATABASE_URL=postgres://…` to use a managed database for the control plane instead of the in-cluster one, `--registry-host <host> --registry-user --registry-token-file` for a provider registry. Without the file: `--dns oci\|aws` with `--dns-zone-id --dns-region` (aws) or `--dns-compartment --dns-tenancy --dns-region --dns-user` (oci), `--internal-lb-subnet <ocid>`, and `--set` for the rest. Explicit settings are recorded, so re-runs need no flags. |
| `shpyrd cluster registry` | The image registry: mode, health, storage used, images held, garbage collection and certificate. `registry gc` reclaims deleted images now (`--wait`). |
| `shpyrd cluster status` | Health of every component, with versions and install times. Exit code 1 when something is not ready. |
| `shpyrd cluster dashboard` | Open the dashboard in the browser, signed in as you through a one-time login ticket (60 s). `--no-open` prints the URL and the link. |
| `shpyrd cluster token` | Print the admin token (Secret `shpyrd-system/shpyrd-admin-token`). `--rotate` replaces it, `--disable`/`--enable` switch it off and on (disable needs a login provider and a platform-admin team). |
| `shpyrd cluster trust-ca` | Install the platform CA in the OS trust store: the development CA (`--ca-dir`) for local clusters, the cluster's own CA when `--context` points at a cloud cluster. Alias `trust`. |
| `shpyrd cluster export` | Render the base stack manifests to a directory for GitOps tooling (`-o`, profile flags). |
| `shpyrd cluster backup` | Back up the platform's state now: an encrypted archive to the configured bucket (`--wait`, default). `cluster backup key` prints the passphrase to keep outside the cluster. |
| `shpyrd cluster backups` | Target, schedule, last good backup, the archives in the bucket and the recent runs. |
| `shpyrd cluster restore` | Restore an archive into a cluster that runs the platform: `--from s3://bucket/prefix[/archive]` (newest by default) or `--file`, `--passphrase-file`, `--credentials-file`/`--endpoint`/`--region`/`--aws-profile` for the bucket, `--project <slug>` (repeatable), `--no-system`, `--overwrite`, `--dry-run`. See [Platform backups](/docs/backups). |
| `shpyrd cluster destroy` | Delete the kind cluster (`--name`, `--yes`); with `--context` on a cloud cluster, remove everything the platform created in the cloud (projects and their data, load balancers, disks) and print the infrastructure command to finish. |
| `shpyrd extensions list` | Extensions known to this build and whether they are enabled on the cluster. |
| `shpyrd extensions enable <name>` | Install the extension's component and restart the server with it (`--set`). Also `cluster init --enable <name>`. |
| `shpyrd extensions disable <name>` | Remove the component (`--yes`); refused while resources of the extension exist. |
| `shpyrd users add <email>` | Create a local account (extension `auth-local`); `--name`, `--password` (prompted when omitted). |
| `shpyrd users list`, `passwd <email>`, `rm <email>` | Manage local accounts. |
| `shpyrd people` | The workspace's people: everyone who has signed in or holds a role, with their role, login method and last sign-in. |
| `shpyrd people role <email> owner\|admin\|member\|none` | Set or remove someone's workspace role (they need not have signed in yet). Naming or demoting an owner takes an owner; the last owner stays. |
| `shpyrd people suspend\|reactivate\|forget <email>` | Switch someone's access off everywhere at once, back on, or remove their sign-in record (role and grants stay). |
| `shpyrd invite <email>` | Invite someone: `--role member\|admin\|owner` (member by default), `--team <name>`. Prints the link once (emailed too when `shpyrd-ctl mail set` was run); signing in with that address accepts it. |
| `shpyrd invitations`, `invitations revoke <email>` | Pending invitations; revoke one. Inviting again makes a new link. |
| `shpyrd teams create <name>` | Create or update a team: `--member <email>`, `--group <idp group>`, `--platform-role platform-admin\|platform-viewer`, `--description`. |
| `shpyrd teams list`, `add <team> <email...>`, `remove <team> <email...>`, `delete <team> --yes` | Manage teams (`--group` for identity provider groups). |
| `shpyrd members add <project> --user <email>\|--team <name> --role reader\|user\|viewer\|developer\|admin` | Grant a role on a project (`reader` opens the app read-only, `user` opens it; see [Sign-in for your app](/docs/app-access)). |
| `shpyrd sso add google\|microsoft\|github\|oidc --client-id ... --client-secret <secret\|@file> [--hosted-domain] [--tenant] [--org] [--issuer] [--label]` | Add a sign-in method of this workspace (its login page only). `sso list`, `sso remove <id>`, `sso platform-methods on\|off`. |
| `shpyrd members list [project]`, `remove <project> --user\|--team` | List and remove grants. |

## Log drains

| Command | What it does |
| --- | --- |
| `shpyrd drains add <url> [--name] [--header "Name: value"]... [--processes web,worker] [--format json\|syslog] --project <slug> \| --cluster` | Forward a project's (or every project's) lines to an HTTPS or syslog receiver (extension `logs-agent`). |
| `shpyrd drains list [--project \| --cluster]` | Drains with delivery status and last delivery. |
| `shpyrd drains remove <name> [--project \| --cluster]` | Remove a drain and its stored headers. |

## Global config vars

| Command | What it does |
| --- | --- |
| `shpyrd globals set KEY=VALUE...` | Set config vars every project receives (platform admins). A "Global config change" release follows in every project that has not opted out. |
| `shpyrd globals unset KEY...` | Remove global config vars. |
| `shpyrd globals list` | Names and when each was set; values are never shown. |

## Sign-in providers

| Command | What it does |
| --- | --- |
| `shpyrd auth oidc set --id <id> --label <text> --issuer <url> --client-id <id> --client-secret <secret\|@file>` | Add or update a company identity provider (extension `auth-oidc`); `--scopes` replaces the extra scopes (`-` for none). Prints the redirect URIs to register. |
| `shpyrd auth oidc list` / `remove <id>` | List or remove providers (roles are kept, they are keyed by email). |
| `shpyrd auth oidc check <id\|issuer-url>` | Fetch the issuer's discovery document: endpoints, PKCE, scopes, claims, sign-out support. |
| `shpyrd auth connector add github\|google\|microsoft\|oidc --client-id ... --client-secret ... [--org] [--hosted-domain] [--tenant] [--issuer]` | A sign-in method through the bundled issuer (extension `auth-local`): GitHub, Google, Microsoft or any OpenID Connect provider; the button appears at once. Also on the dashboard's Workspace › Sign-in tab. |
| `shpyrd auth connector list` / `remove <id>` | List or remove connectors. |

## Email

| Command | What it does |
| --- | --- |
| `shpyrd-ctl mail set --host <smtp> --from "<name> <addr>" [--port N] [--user U --password <secret\|@file>] [--tls\|--plain]` | The SMTP sender the platform uses for invitations (extension `mail`); STARTTLS on 587 by default. The password is stored in the cluster and never printed. |
| `shpyrd-ctl mail status`, `test <address>`, `unset` | Show the settings (without the password), send a test message from the server, remove the settings. |

## Projects

| Command | What it does |
| --- | --- |
| `shpyrd projects create "<name>"` | Create the project. The name is free text ("My Shop"); its **slug** (`my-shop`) is derived from it and identifies the project in `--project`, URLs and the hostname. `--slug` chooses it, `--domain` adds custom domains, `--save` writes `shpyrd.yaml`. (`shpyrd apps` still works as an alias.) New projects ask visitors to sign in; `--public` makes a site anyone can open. |
| `shpyrd projects rename <slug> "<name>"` | Change the display name. The slug never changes. |
| `shpyrd projects list` | Table of projects: slug, name, phase, release, URL, age. |
| `shpyrd projects info <slug>` | Phase and message, URL, build digest, source, processes (with sizes and failing reasons), recent releases, and every resource of the project (app, attached resources, volumes). |
| `shpyrd projects destroy <name>` | Delete the project and its namespace (`--yes`). |

## Deploying and running

| Command | What it does |
| --- | --- |
| `shpyrd deploy` | Archive the committed tree of the current directory, upload, build and release. `--working-tree` deploys the directory as is; `--git <url> --ref <rev> --path <dir>` builds from Git; `--dockerfile [path]` builds the Dockerfile (auto-detected for local deploys); `--image <ref>` runs a prebuilt image; `--no-wait` returns immediately. Applies `shpyrd.yaml` (processes, sizes, build, domains). |
| `shpyrd scale web=N worker=M` | Set instance counts per process type. |
| `shpyrd resize web=SIZE worker=SIZE` | Set instance sizes per process type (a release). |
| `shpyrd sizes list` | The cluster's instance size catalog with kind, cpu, burst and memory. |
| `shpyrd sizes set <name> --kind shared\|dedicated --cpu <cores> --memory <bytes> [--default]` | Add or change a size; processes using it are resized. |
| `shpyrd sizes delete <name>`, `shpyrd sizes default <name>` | Remove a size (not the default), choose the default. |
| `shpyrd secrets set K=V ...` | Set config vars (new release, rolling restart). |
| `shpyrd secrets unset K ...` | Remove config vars. |
| `shpyrd secrets list` | Names and last-updated times, plus variables provided by attached resources. Values are never printed. |
| `shpyrd shell [-- cmd...]` | Interactive shell in a running instance (`--process`, `--instance web.2`); with a command, runs it and returns its exit code. |
| `shpyrd run <cmd...>` | One-off instance of the current release with the config vars: streams output, returns the exit code, removes the instance. `--size`, `--detach`. |
| `shpyrd volumes create <name> --size 5Gi` | Create a persistent volume in the project (`--class`, `--shared`, `--from-snapshot`). Cloud profiles round the size up to the provider's minimum and say so. |
| `shpyrd volumes list` | Volumes with size, mode, status and what mounts them. |
| `shpyrd volumes resize <name> --size 10Gi` | Grow a volume (when the storage class allows expansion). |
| `shpyrd volumes delete <name>` | Delete a volume and its data (`--yes`; `--force` while mounted). |
| `shpyrd volumes snapshot <volume> [--name <snapshot>]` | Take a snapshot of the volume (where the profile supports snapshots; `--no-wait`). |
| `shpyrd volumes snapshots <volume>` | List the volume's snapshots (also `snapshot list`); `snapshot rm <volume> <snapshot> --yes` deletes one. |
| `shpyrd volumes restore <volume> --from <snapshot> [--to <new-volume>]` | Restore a snapshot into a new volume, or in place (`--yes`: the mounting instances stop while the disk is replaced). |
| `shpyrd object-storage list` | Buckets of the platform's object store with usage (extension object-storage). |
| `shpyrd pg create <name> --project <p>` | Create a PostgreSQL database (extension `postgres`): `--backups`/`--retention`/`--backup-schedule`, `--version`, `--size`, `--storage`, `--instances`. |
| `shpyrd pg list\|info\|psql\|delete` | Manage databases; `psql <name> -- <args>` opens psql on the primary; delete is refused while attached (`--force`). |
| `shpyrd pg backups enable\|disable\|list <name>` | Backups of a database (needs extension `object-storage`): continuous WAL archiving and a scheduled base backup (`--retention 14d`, `--schedule "0 2 * * *"`); list shows the base backups and the recovery window. |
| `shpyrd pg backup <name>` | Take a base backup now. |
| `shpyrd pg restore <name> --as <new> [--to <RFC 3339>]` | Restore into a new database at a point in time (latest when omitted); attach the app to it when ready. |
| `shpyrd redis create <name> --project <p>` | Create a Valkey or Redis store (extension `redis`): `--engine`, `--version`, `--size`, `--persistent`, `--storage`. |
| `shpyrd redis list\|info\|cli\|delete` | Manage stores; `cli <name> -- <args>` runs valkey-cli or redis-cli. |
| `shpyrd attach <resource>` | Attach a database or store to the app as config vars (`--kind` when ambiguous, `--prefix`). A release. |
| `shpyrd detach <resource>` | Remove the attachment (a release). |
| `shpyrd logs` | Tail logs of every instance (`web.1`, `worker.2`...). `-f` follow, `-p <process>`, `-n <lines>`, `--build` for the latest build output. |
| `shpyrd releases` | Release history with digests and descriptions. |
| `shpyrd rollback [N]` | Re-release N (default: the previous release) with its build and config vars. Refused while another release is rolling out unless `--force`; `--no-wait`. |
| `shpyrd redeploy` | Try the current release again without a new release: new instances of it, or, after a failed build or with `--rebuild`, the same source built again. `--no-wait`. |
| `shpyrd domains add <host>` | Serve the project at a hostname you own; prints the DNS record to create (CNAME to the project hostname, or A to the front door) and waits until it serves (`--no-wait`). |
| `shpyrd domains list`, `rm <host>` | Custom domains with DNS and certificate state; stop serving one. |
| `shpyrd exposure internal\|external` | Which front door serves the project on cloud profiles (public or private load balancer). Release-free. |
| `shpyrd access [set public\|authenticated\|identified]` | Who may open the app: sign-in required (the default), public, or public with signed-in visitors identified; without `set`, shows the mode and the roles that open it. |
| `shpyrd open` | Open the project URL in the browser. |

## Where things are

| | |
| --- | --- |
| `~/.shpyrd/ca/` | development root CA (`rootCA.pem`, key); `~/.shpyrd/clusters/<name>/` the CA fetched from a cloud cluster |
| `~/.kube/config` | kind writes the `kind-shpyrd` context here |
| namespace `shpyrd-system` | server, registry (with its credential and certificate), node trust DaemonSet, ExternalDNS, admin token, install record, sessions mirror, Dex and its accounts when `auth-local` is enabled |
| namespace `app-<name>` | one per project (label `shpyrd.io/project`): App, Volumes and their claims, Deployments, Services, Ingress and one Certificate per host that needs one, kpack Image and Builds or BuildKit Jobs, config var Secret, `<app>-bindings` and release snapshots |
| `contrib/oci/` | Terraform for the Oracle Cloud network and cluster, `kubeconfig.sh`, `tunnel.sh` |
