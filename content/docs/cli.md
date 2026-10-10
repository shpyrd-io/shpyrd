---
title: CLI reference
description: Every shpyrd command and its flags.
---

`shpyrd` is the CLI for people who deploy and run projects: it signs in to your workspace with `shpyrd login` - on shpyrd cloud, `https://acme.shpyrd.cloud` - and needs no kubeconfig: every developer command speaks the workspace API. `shpyrd-ctl`, installed alongside it, is for running shpyrd yourself: the operator who installs and runs the platform (the cluster, extensions, accounts, backups). With a kubeconfig named on the command line (`--context`, `--kubeconfig`) it goes through the cluster instead, as `shpyrd-ctl` always does. Project commands take `--project <slug>` or read `project:` from `shpyrd.yaml` in the current directory; `-v` prints verbose output. Commands contributed by extensions explain themselves when the extension is not enabled: `pg` and `redis` (a project's resources) live in `shpyrd`; `users`, `auth` and `object-storage` (the platform's) in `shpyrd-ctl`. {% .lead %}

## For scripts and agents

Every command takes `--json`: the result is printed as one JSON document on stdout and nothing else (progress and hints go to stderr), with the shapes the workspace API uses, so a script or an agent reads it instead of parsing a table. `--jq <expression>` filters that document the way `gh --jq` does: strings print raw, everything else as compact JSON. Interactive commands (`shell`, `run`, `pg psql`, `redis cli`, `logs`) stream as they always did.

```sh
shpyrd projects list --json
shpyrd projects list --jq '.[].slug'
shpyrd releases --project shop --jq '.[-1].number'
shpyrd deploy --json | jq .release
```

Inputs can come from files too. `shpyrd secrets set` and `shpyrd globals set` take `--from-file <path>`: a dotenv file (one `KEY=VALUE` per line, `#` comments, optional `export`, single or double quotes) or, when it starts with `{`, a JSON object of strings; `-` reads stdin, and `KEY=VALUE` arguments on the same command line win over the file. Flags that carry a secret (`login --token`, `shpyrd-ctl users add --password`, `mail set --password`, `sso add --client-secret`) take `@path` to read the value from a file instead of the shell history.

```sh
shpyrd secrets set --from-file .env.production --project shop
shpyrd secrets set --from-file - --project shop < vars.json
shpyrd login --url https://acme.shpyrd.cloud --token @token.txt
```

## Signing in

| Command | What it does |
| --- | --- |
| `shpyrd login --signup` | Create an account and your first workspace on shpyrd cloud from the terminal: the browser opens the signup with a code; you prove your email and name the workspace there, and when its door answers the CLI is signed in to it. `shpyrd login` with no URL at a terminal asks which of the two you want. `--signup-url` (or `SHPYRD_SIGNUP_URL`) points at another signup. |
| `shpyrd login --url <workspace> [--token <token>]` | Sign the CLI in to a workspace (`https://acme.shpyrd.cloud`) and make it the **current** one; the credential is kept in `~/.shpyrd/sessions.json`. Without `--token` the browser opens the workspace's sign-in: approve the code the terminal shows and the CLI is signed in as you for 30 days (`--no-browser` prints the link instead, for a shell over SSH); That creates a **session token**, listed under Workspace → API tokens as `CLI on <host>` and revoked there. With `--token`, a personal API token (below) or, for the operator, the admin token from `shpyrd-ctl cluster token`. `SHPYRD_URL` and `SHPYRD_TOKEN` work without a saved session: set them in CI. A token the workspace rejects is not saved. |
| `shpyrd use [workspace]` | List the workspaces you are signed in to (`*` marks the current one), or switch. Commands talk to the current workspace; with several sessions and none current they ask you to pick. |
| `shpyrd version` | The CLI's version (`shpyrd-ctl version` too). |
| `shpyrd whoami` | Who you are at the current workspace, checked live; fails when the token expired or was revoked. |
| `shpyrd logout` | Forget the current workspace's credential (or `--url` another's). |
| `shpyrd tokens create <name>` | Create an API token for CI or another machine: `--platform-role platform-viewer\|platform-admin` or `--project <slug> --role reader\|user\|viewer\|developer\|admin`, `--expires 90d`. The value is printed once. A token never carries more than you hold at the moment it is used, and a token cannot create tokens: run this signed in as yourself (or with the admin token), or use the dashboard's **Workspace › API tokens** page. |
| `shpyrd tokens list`, `tokens revoke <id>` | Your tokens with role, expiry and last use (platform admins see everyone's); a session token is listed as `session (your roles)`. Revocation is immediate. |

Every developer command works signed in with `shpyrd login`, with no kubeconfig: `run`, `shell -- <cmd>`, `pg psql` and `redis cli` go through the web terminal's bridge, the server starting the one-off instance or picking the database's pod. On shpyrd cloud, databases, caches and custom domains are also at hand in the dashboard: the project's **Resources** and **Domains** pages.

## Workspace and people

For a workspace's owners and admins, on shpyrd cloud or on a cluster you run. They live in `shpyrd` and work signed in.

| Command | What it does |
| --- | --- |
| `shpyrd people` | The workspace's people: everyone who has signed in or holds a role, with their role, login method and last sign-in. |
| `shpyrd people role <email> owner\|admin\|member\|none` | Set or remove someone's workspace role (they need not have signed in yet). Naming or demoting an owner takes an owner; the last owner stays. |
| `shpyrd people suspend\|reactivate\|forget <email>` | Switch someone's access off everywhere at once, back on, or remove their sign-in record (role and grants stay). |
| `shpyrd invite <email>` | Invite someone: `--role member\|admin\|owner` (member by default), `--team <name>`. Prints the link once (emailed too when `shpyrd-ctl mail set` was run); signing in with that address accepts it. |
| `shpyrd invitations`, `invitations revoke <email>` | Pending invitations; revoke one. Inviting again makes a new link. |
| `shpyrd teams create <name>` | Create or update a team: `--member <email>`, `--group <idp group>`, `--platform-role platform-admin\|platform-viewer`, `--description`. |
| `shpyrd teams list`, `add <team> <email...>`, `remove <team> <email...>`, `delete <team> --yes` | Manage teams (`--group` for identity provider groups). |
| `shpyrd members add <project> --user <email>\|--team <name> --role reader\|user\|viewer\|developer\|admin` | Grant a role on a project (`reader` opens the app read-only, `user` opens it; see [Sign-in for your app](/docs/app-access)). |
| `shpyrd projects describe <project> [--description "…"] [--icon name] [--color name] [--icon-file path] [--featured\|--unfeatured]` | What the launcher shows for a project: the line under its name, its symbol (by the name lucide gives it) and the symbol's colour, or an image of its own (`.svg`, `.png` or `.webp`; empty removes it), and whether it is shown first and larger. |
| `shpyrd workspace` | The workspace you are signed in to: name, address, dashboard, owners. `workspace address <label>` moves it (owners; the old address redirects 30 days); `workspace domains add\|verify\|primary\|alias\|remove <host>` manage custom domains. |
| `shpyrd sso add google\|microsoft\|github\|oidc --client-id ... --client-secret <secret\|@file> [--hosted-domain] [--tenant] [--org] [--issuer] [--label] [--id]` | Add a sign-in method of this workspace (its login page only; enterprise). `sso list`, `sso remove <id>`, `sso platform-methods on\|off`. |
| `shpyrd members list [project]`, `remove <project> --user\|--team` | List and remove grants. |

## Cluster

Self-hosted: operator commands, for running shpyrd yourself (on shpyrd cloud, we run the cluster). They live in `shpyrd-ctl` (installed alongside `shpyrd` by Homebrew and the release archives) and also answer as `shpyrd cluster …` when a kubeconfig is available.

| Command | What it does |
| --- | --- |
| `shpyrd cluster create` | Create a kind cluster and install the base stack. `--name`, `--workers`, `--image`, `--http-port`, `--https-port`, `--domain`, `--profile`, `--skip`, `--only`, `--set SHPYRD_X=y`, `--enable`/`--disable <extension>`, `--front-door kind\|caddy\|auto`, `--local-dns` (macOS), `--no-init`. |
| `shpyrd cluster init` | Install or upgrade the base stack on the current context. Same profile flags; `--yes` for non-kind contexts. Cloud profiles: `--profile oci` or `--profile aws` with `--vars-file <name>.vars` (written by `contrib/*/terraform`: domain, addresses, zone, storage; flags and `--set` win over it), `--set SHPYRD_ACME_EMAIL=...`, `--platform-exposure internal`, `--dns-key-file` (OCI key), `--backup-target s3://bucket/prefix --backup-credentials-file <file>` for [platform backups](/docs/backups), `--set SHPYRD_DATABASE_URL=postgres://…` to use a managed database for the control plane instead of the in-cluster one, `--registry-host <host> --registry-user --registry-token-file` for a provider registry. Without the file: `--dns oci\|aws` with `--dns-zone-id --dns-region` (aws) or `--dns-compartment --dns-tenancy --dns-region --dns-user` (oci), `--internal-lb-subnet <ocid>`, and `--set` for the rest. Explicit settings are recorded, so re-runs need no flags. |
| `shpyrd cluster registry` | The image registry: mode, health, storage used, images held, garbage collection and certificate. `registry gc` reclaims deleted images now (`--wait`). |
| `shpyrd cluster status` | Health of every component, with versions and install times, and a warning for the platform's pods running outside the platform pool on a cloud cluster ([Node pools](/docs/oracle-cloud#node-pools)). Exit code 1 when something is not ready. |
| `shpyrd cluster dashboard` | Open the dashboard in the browser, signed in as you through a one-time login ticket (60 s). `--no-open` prints the URL and the link. |
| `shpyrd cluster token` | Print the admin token (Secret `shpyrd-system/shpyrd-admin-token`). `--rotate` replaces it, `--disable`/`--enable` switch it off and on (disable needs a login provider and a platform-admin team). |
| `shpyrd cluster trust-ca` | Install the platform CA in the OS trust store: the development CA (`--ca-dir`) for local clusters, the cluster's own CA when `--context` points at a cloud cluster. Alias `trust`. `cluster untrust-ca` (alias `untrust`) removes it. |
| `shpyrd cluster export` | Render the base stack manifests to a directory for GitOps tooling (`-o`, profile flags). |
| `shpyrd cluster backup` | Back up the platform's state now: an encrypted archive to the configured bucket (`--wait`, default). `cluster backup key` prints the passphrase to keep outside the cluster. |
| `shpyrd cluster backups` | Target, schedule, last good backup, the archives in the bucket and the recent runs. |
| `shpyrd cluster restore` | Restore an archive into a cluster that runs the platform: `--from s3://bucket/prefix[/archive]` (newest by default) or `--file`, `--passphrase-file`, `--credentials-file`/`--endpoint`/`--region`/`--aws-profile` for the bucket, `--project <slug>` (repeatable), `--no-system`, `--overwrite`, `--dry-run`. See [Platform backups](/docs/backups). |
| `shpyrd cluster destroy` | Delete the kind cluster (`--name`, `--yes`); with `--context` on a cloud cluster, remove everything the platform created in the cloud (projects and their data, load balancers, disks) and print the infrastructure command to finish. `--keep-cluster` removes the platform and keeps the cluster, for a fresh `cluster init`. |
| `shpyrd cluster snapshots take --class <storage-class>` | Snapshot every block disk of that storage class with the provider, keeping the newest (`--keep 7`); `--system` adds the platform's own disks, `--namespace-prefix p-` selects namespaces by name, `--wait 3m`. `cluster snapshots list` shows them, newest first. |
| `shpyrd extensions list` | Extensions known to this build and whether they are enabled on the cluster. |
| `shpyrd extensions enable <name>` | Install the extension's component and restart the server with it (`--set`). Also `cluster init --enable <name>`. |
| `shpyrd extensions disable <name>` | Remove the component (`--yes`); refused while resources of the extension exist. |
| `shpyrd-ctl users add <email>` | Create a local account (extension `auth-local`); `--name`, `--password <secret\|@file>` (prompted when omitted), or `--invite` for an account without a password, set later through the reset flow or a workspace invite. |
| `shpyrd-ctl users list`, `passwd <email>`, `rm <email>` | Manage local accounts. |

## Log drains

| Command | What it does |
| --- | --- |
| `shpyrd drains add <url> [--name] [--header "Name: value"]... [--processes web,worker] [--format json\|syslog] --project <slug> \| --workspace <slug> \| --cluster` | Forward a project's, a workspace's or every project's lines to an HTTPS or syslog receiver (extension `logs-agent`). |
| `shpyrd drains list [--project \| --workspace \| --cluster]` | Drains with delivery status and last delivery. |
| `shpyrd drains remove <name> [--project \| --workspace \| --cluster]` | Remove a drain and its stored headers. |

## Global config vars

| Command | What it does |
| --- | --- |
| `shpyrd globals set KEY=VALUE...` | Set config vars every project of the workspace receives (workspace admins; `--workspace` names one over a kubeconfig). A "Global config change" release follows in every project that has not opted out. |
| `shpyrd globals unset KEY...` | Remove global config vars. |
| `shpyrd globals list` | Names and when each was set; values are never shown. |

## Sign-in providers

Self-hosted: the sign-in methods of a cluster you run yourself. Email and password (extension `auth-local`) is the platform's own; GitHub, Google, Microsoft and any OpenID Connect provider are enterprise features, on with a license (`shpyrd-ctl license set`) and always on shpyrd cloud. A workspace's own sign-in is `shpyrd sso` (above) or the dashboard's **Workspace › Sign-in** page.

| Command | What it does |
| --- | --- |
| `shpyrd-ctl auth connector add github\|google\|microsoft\|oidc --client-id ... --client-secret <secret\|@file> [--realm console\|platform] [--org] [--hosted-domain] [--tenant] [--issuer] [--label] [--id]` | A sign-in method through the bundled issuer (enterprise): on the console's login page (`--realm console`) or among the methods every workspace offers (`platform`, the default); the button appears at once. Also on the console's Sign-in page. |
| `shpyrd-ctl auth connector list` / `remove <id> [--realm]` | List or remove connectors. |

## The platform

Self-hosted: the operator's, in `shpyrd-ctl`.

| Command | What it does |
| --- | --- |
| `shpyrd-ctl workspace status` | The platform's workspace: its address, its limits and its sleep defaults, and what its projects use against them. |
| `shpyrd-ctl workspace limits [--projects N --instances N --cpu 2 --memory 1Gi --storage 10Gi \| --none]` | What the workspace may use; the flags given are set over the limits in force. A project, a deploy or a database beyond them is refused with the limit it would cross. |
| `shpyrd-ctl workspace sleep [--apps 15m --resuming page\|wait] [--databases 30m] [--none]` | The sleep its apps and databases take when they set none of their own (enterprise: auto sleep). `off` for either turns it off. |
| `shpyrd-ctl console-users list` | Who may open the console: its own list, apart from every workspace's people. Each is an admin of the console. |
| `shpyrd-ctl console-users add <email> [--password <secret\|@file>]` / `remove <email>` | Add someone to the list, with an email and password account when `--password` is given; with sign-in through a provider, the email it gives must be on the list. Removing yourself is refused. |
| `shpyrd-ctl license set <file>` / `license status` | Install the enterprise license (a file from shpyrd): GitHub, Google, Microsoft and OIDC sign-in, auto sleep, costs and the MCP server switch on until it expires, and off on that day. |
| `shpyrd-ctl license renew` / `license billing` | A license bought online renews by itself a week before it expires, sending the last 30 days of what the cluster used and cost; `renew` does it now. `billing` prints a link of one use to your account at shpyrd's billing (also the console's Settings › License › Open billing). |
| `shpyrd-ctl costs [--from YYYY-MM-DD --to YYYY-MM-DD] [--kind estimated\|real\|usage] [--by project\|process\|resource\|service]` | What the cluster uses and costs (enterprise): OpenCost's estimate (extension `opencost`), the provider's bill (`costs oci set`) or the usage the platform measured. |
| `shpyrd-ctl costs drains add <name> <url> [--header "Name=value"]...` / `list` / `remove <name>` | Send every cost line, as it is written or revised, to an HTTPS receiver: by workspace and project (their UUIDs, with the project's name), process and resource. |
| `shpyrd-ctl opencost` | Whether the OpenCost extension is installed and answering. |
| `shpyrd-ctl object-storage list` | Buckets of the platform's object store with usage (extension `object-storage`; alias `buckets`). |
| `shpyrd-ctl costs oci set --tenancy … --user … --fingerprint … --region … --key @oci.pem` / `status` / `remove` | Read the real bill from Oracle Cloud's Usage API, resource by resource. |

## Email

Self-hosted: the platform's sender, set by the operator.

| Command | What it does |
| --- | --- |
| `shpyrd-ctl mail set --host <smtp> --from "<name> <addr>" [--port N] [--user U --password <secret\|@file>] [--tls\|--plain]` | The SMTP sender the platform uses for invitations (extension `mail`); STARTTLS on 587 by default. The password is stored in the cluster and never printed. |
| `shpyrd-ctl mail status`, `test <address>`, `unset` | Show the settings (without the password), send a test message from the server, remove the settings. |

## Projects

| Command | What it does |
| --- | --- |
| `shpyrd projects create "<name>"` | Create the project. The name is free text ("My Shop"); its **slug** (`my-shop`) is derived from it and identifies the project in `--project`, URLs and the hostname. On shpyrd cloud the app answers at `https://<workspace>-<slug>.shpyrd.app`; on a cluster you run, at `https://<slug>.<domain>`. `--slug` chooses it, `--domain` adds custom domains, `--save` writes `shpyrd.yaml`. (`shpyrd apps` still works as an alias.) New projects ask visitors to sign in; `--public` makes a site anyone can open. |
| `shpyrd projects rename <slug> "<name>"` | Change the display name. `--slug <new>` changes the slug and the address too; the old address redirects for 30 days (signed in; projects in a `p-<id>` namespace). |
| `shpyrd projects list` | Table of projects: slug, name, phase, release, URL, age. |
| `shpyrd projects info <slug>` | Phase and message, URL, build digest, source, processes (with sizes and failing reasons), recent releases, and every resource of the project (app, attached resources, volumes). |
| `shpyrd projects destroy <name>` | Delete the project and its namespace (`--yes`). |

## Deploying and running

| Command | What it does |
| --- | --- |
| `shpyrd deploy` | Archive the committed tree of the current directory, upload, build and release. From a package of an npm, pnpm or Yarn workspace it archives the workspace's root and builds that package ([Monorepos and workspaces](/docs/deploying#monorepos-and-workspaces)). `--working-tree` deploys the directory as is; `--git <url> --ref <rev> --path <dir>` builds from Git; `--dockerfile [path]` builds the Dockerfile (auto-detected for local deploys); `--image <ref>` runs a prebuilt image; `--no-wait` returns immediately; `--save` writes what the build profile or workspace detection inferred into `shpyrd.yaml`. Applies `shpyrd.yaml` (processes, sizes, build, domains). |
| `shpyrd scale web=N worker=M` | Set instance counts per process type. |
| `shpyrd resize web=SIZE worker=SIZE` | Set instance sizes per process type (a release). |
| `shpyrd sizes list` | The instance sizes to choose from, in three lists with the same names: processes, Postgres and Redis (with their connections), each with its default (`shpyrd sizes` alone does the same). `--json` answers it as `GET /api/sizes` does. |
| `shpyrd-ctl sizes set <name> --kind shared\|dedicated --cpu <cores> --memory <bytes> [--default] [--description]` | Operator: add or change a size; processes using it are resized. `--for postgres\|redis` edits those lists instead, with `--connections <n>` (max_connections, maxclients). |
| `shpyrd-ctl sizes delete <name>`, `shpyrd-ctl sizes default <name>` | Operator: remove a size (not the default; processes naming it fall back to the default), choose the default. Both take `--for postgres\|redis`. |
| `shpyrd secrets set K=V ...` | Set config vars (new release, rolling restart). |
| `shpyrd secrets unset K ...` | Remove config vars. |
| `shpyrd secrets list` | Names and last-updated times, plus variables provided by attached resources. Values are never printed. |
| `shpyrd shell [-- cmd...]` | Interactive shell in a running instance (`--process`, `--instance web.2`); with a command, runs it and returns its exit code. |
| `shpyrd run <cmd...>` | One-off instance of the current release with the config vars: streams output, returns the exit code, removes the instance. `--size`, `--detach`. Signed in, the server starts the instance and attaches the terminal through the web terminal's bridge; piped input reaches the command and ends when the pipe does. |
| `shpyrd volumes create <name> --size 5Gi` | Create a persistent volume in the project (`--class`, `--shared`, `--from-snapshot`). Cloud profiles round the size up to the provider's minimum and say so. |
| `shpyrd volumes list` | Volumes with size, mode, status and what mounts them. |
| `shpyrd volumes resize <name> --size 10Gi` | Grow a volume (when the storage class allows expansion). |
| `shpyrd volumes delete <name>` | Delete a volume and its data (`--yes`; `--force` while mounted). |
| `shpyrd volumes snapshot <volume> [--name <snapshot>]` | Take a snapshot of the volume (where the profile supports snapshots; `--no-wait`). |
| `shpyrd volumes snapshots <volume>` | List the volume's snapshots (also `snapshot list`); `snapshot rm <volume> <snapshot> --yes` deletes one. |
| `shpyrd volumes restore <volume> --from <snapshot> [--to <new-volume>]` | Restore a snapshot into a new volume, or in place (`--yes`: the mounting instances stop while the disk is replaced). |
| `shpyrd pg create <name> --project <p>` | Create a PostgreSQL database (extension `postgres`): `--backups`/`--retention`/`--backup-schedule`, `--version`, `--size` (a Postgres size; the smallest when omitted), `--storage`, `--instances`. |
| `shpyrd pg resize <name> <size> --project <p>` | Give a database another Postgres size; its instances restart one at a time. |
| `shpyrd pg list\|info\|psql\|delete` | Manage databases; `psql <name> -- <args>` opens psql on the primary; delete is refused while attached (`--force`). |
| `shpyrd pg backups enable\|disable\|list <name>` | Backups of a database (needs extension `object-storage`): continuous WAL archiving and a scheduled base backup (`--retention 14d`, `--schedule "0 2 * * *"`); list shows the base backups and the recovery window. |
| `shpyrd pg backup <name>` | Take a base backup now. |
| `shpyrd pg sleep <name> --after 30m` | Stop the database after 30 min without client connections and wake it on the first one (about 30–40 s on a cloud block volume; volume and data kept; single-instance databases only). `--after off` disables — also when the workspace has a default for databases; `--after default` follows the workspace again. Sleep is an enterprise feature (auto sleep). Attached apps are re-released once. See [Databases](/docs/databases#sleep). |
| `shpyrd pg suspend <name>`, `shpyrd pg resume <name>` | Stop a database now and refuse connections until resumed (data kept), and bring it back. |
| `shpyrd pg restore <name> --as <new> [--to <RFC 3339>]` | Restore into a new database at a point in time (latest when omitted); `--size` and `--storage` default to the source's. Attach the app to it when ready. |
| `shpyrd redis create <name> --project <p>` | Create a Valkey or Redis store (extension `redis`): `--engine`, `--version`, `--size` (a Redis size; the smallest when omitted), `--persistent`, `--storage`. |
| `shpyrd redis resize <name> <size> --project <p>` | Give a store another Redis size; it restarts (a cache comes back empty). |
| `shpyrd redis list\|info\|cli\|delete` | Manage stores; `cli <name> -- <args>` runs valkey-cli or redis-cli. |
| `shpyrd attach <resource>` | Attach a database or store to the app as config vars (`--kind` when ambiguous, `--prefix`). A release. |
| `shpyrd detach <resource>` | Remove the attachment (a release). |
| `shpyrd logs` | Tail logs of every instance (`web.1`, `worker.2`...). `-f` follow, `-p <process>`, `-n <lines>` per instance (100 by default, `-1` for all), `--build` for the latest build output. `--json` prints the JSON lines as the app wrote them; `--pretty` renders them readably (the default at a terminal). |
| `shpyrd releases` | Release history with digests and descriptions. |
| `shpyrd rollback [N]` | Re-release N (default: the previous release) with its build and config vars. Refused while another release is rolling out unless `--force`; `--no-wait`. |
| `shpyrd redeploy` | Try the current release again without a new release: new instances of it, or, after a failed build or with `--rebuild`, the same source built again. `--no-wait`. |
| `shpyrd domains add <host>` | Serve the project at a hostname you own; prints the DNS record to create (CNAME to the project hostname, or A to the front door) and waits until it serves (`--no-wait`). |
| `shpyrd domains list`, `rm <host>` | Custom domains with DNS and certificate state; stop serving one. |
| `shpyrd exposure internal\|external` | Which front door serves the project on cloud profiles (public or private load balancer). Release-free. |
| `shpyrd allow [add\|remove project <slug>\|platform actions\|mcp]` | Which callers inside the cluster may reach the project: another project of the workspace, the server acting for a user (`actions`), or the MCP connector (`mcp`). Projects are isolated by default; without a subcommand, shows the list. Release-free. See [Sign-in for your app](/docs/app-access). |
| `shpyrd access [set public\|authenticated\|identified]` | Who may open the app: sign-in required (the default), public, or public with signed-in visitors identified; without `set`, shows the mode and the roles that open it. |
| `shpyrd sleep <project> --after 15m [--resuming page\|wait]` | Scale the web process to zero after a quiet period (5m to 24h); the first request wakes it (about 6–7 s), either behind a branded "resuming" page or by holding the connection. `--after off` disables — also when the workspace has a default; `--after default` follows the workspace again. Needs the `sleep` extension on the cluster (`shpyrd-ctl extensions enable sleep`) and auto sleep, an enterprise feature; the command says so otherwise. Nothing sleeps unless a project or its workspace says so. While a project sleeps, its page and `projects info` say so instead of "0 of 1 running". |
| `shpyrd open` | Open the project URL in the browser. |

## Where things are

Self-hosted: where things are on your machine and on a cluster you run yourself.

| | |
| --- | --- |
| `~/.shpyrd/ca/` | development root CA (`rootCA.pem`, key); `~/.shpyrd/clusters/<name>/` the CA fetched from a cloud cluster |
| `~/.kube/config` | kind writes the `kind-shpyrd` context here |
| namespace `shpyrd-system` | server, registry (with its credential and certificate), node trust DaemonSet, ExternalDNS, admin token, install record, sessions mirror, Dex and its accounts when `auth-local` is enabled |
| namespace `p-<id>` | one per project, label `shpyrd.io/project` (older projects: `app-<slug>`): App, Volumes and their claims, Deployments, Services, Ingress and one Certificate per host that needs one, kpack Image and Builds or BuildKit Jobs, config var Secret, `<app>-bindings` and release snapshots |
| `contrib/oci/` | Terraform for the Oracle Cloud network and cluster, `kubeconfig.sh`, `tunnel.sh` |

## Metrics

```sh
shpyrd metrics --project shop --process web
shpyrd metrics --project shop --by instance --mode total --range 6h --json
shpyrd pg metrics db --project shop
shpyrd redis metrics cache --project shop --range 24h
```

`metrics` uses the project from `shpyrd.yaml` when `--project` is omitted.
`--process` (`-p`) selects a process type; HTTP throughput and latency remain
project-wide. CPU and memory default to percentages; `--mode total` shows cores
and bytes. `--by instance` supports `--agg none|sum|avg|max` and `--replaced`.

Database metrics live under `pg` and `redis`, require `--project`, and report
CPU (cores), memory (bytes), network (bytes/s), and storage used/capacity (bytes),
summed across the resource's instances. These are infrastructure metrics;
connections, transactions, cache hits and other database internals are not included.
Storage needs a persistent volume with kubelet volume statistics.

All three commands accept `--range 15m|1h|6h|24h|7d` (default `1h`). Text output
shows each series' latest sample and its UTC timestamp. `--json` returns the full
API time series; `--jq` filters it. Missing samples show `No data`, and failed
queries show their errors. Metrics require monitoring configured on the server;
database commands also require the server version providing resource metrics.

### Time series for charts and agents

Use `--json` to retrieve every available sample in the selected window:

```sh
shpyrd metrics --project shop --process web --mode total --range 6h --json
shpyrd pg metrics db --project shop --range 6h --json
shpyrd redis metrics cache --project shop --range 6h --json
```

All three use the same response shape. This illustrative excerpt shows one chart
with two samples; actual responses include the other charts and available points:

```json
{
  "range": "6h",
  "step": 360,
  "charts": [{
    "id": "cpu",
    "title": "CPU",
    "unit": "cores",
    "kind": "line",
    "instanceCapable": false,
    "series": [{
      "name": "all",
      "points": [[1791298800, 0.18], [1791299160, 0.24]]
    }]
  }],
  "releases": []
}
```

Each point is `[unix_timestamp_seconds, numeric_value]`. Plot timestamps on the
horizontal axis and values on the vertical axis, using `unit` for its label and
`series.name` for the legend. `step` is the query interval in seconds. The server
targets roughly 60 intervals per window, with a minimum interval of 60 seconds:
15m and 1h use 60s, 6h uses 360s, 24h uses 1440s, and 7d uses 10080s. These are
sampled query results, not every raw monitoring scrape.

`kind` suggests line, stacked or step rendering. `reference` and `burst`, when
present on a series, describe allocation reference lines. `releases` provides
project deployment markers (`number`, `time` in Unix seconds, and `label`).
`error` marks a failed chart query and `note` explains omitted data. Empty series
or missing timestamps represent unavailable data, not zero usage; preserve gaps.

To extract a chart while retaining its units and all its series:

```sh
shpyrd pg metrics db --project shop --range 6h --jq '.charts[] | select(.id == "cpu")'
```
