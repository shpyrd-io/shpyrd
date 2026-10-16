# Connect GitHub: the GitHub App, part 3 (RFC-0054)

Date: 2026-10-16
Status: approved in conversation, awaiting review of this text
Builds on:
- `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md` §3
- PR #223 (git credentials)
- PR #225 (the source watcher)

This branch, `feat/github-app-connect`, is stacked on #225. Where this file and the earlier spec differ, this file wins.

## Intent

A workspace connects GitHub once. After that, anyone in it can pick a repository from a list. Private repositories build without deploy keys or tokens, and every push deploys through the App's own webhook, so there is nothing to paste per project.

Part 4 follows separately. It covers checks and Deployments written back to GitHub, watch paths and wait-for-CI.

Done when:
- **An operator** registers a cluster's App with one command and one click on GitHub.
- **A workspace admin** connects an installation from the dashboard or the CLI, and the server verifies that the person can see that installation.
- **"New project → From GitHub"** creates a project from a private repository picked from a list. It builds, and pushes deploy it.
- **Builds clone** with installation tokens that last an hour and are scoped to one repository. Deploy keys and tokens still work, after the App.
- **A deleted or suspended installation** is noticed and audited.
- **Verified for real:** the run on the kind cluster against a real GitHub App and a private test repository (the maintainer provides both) has screenshots.

Out of scope:
- checks, Deployments, watch paths and wait-for-CI (part 4);
- pull request previews;
- GitHub Enterprise Server (the API base URL can be configured, but nothing more);
- a relay for several clusters (shpyrd cloud is one cluster).

## Decisions

### The App's credentials

- **The Secret:** `shpyrd-github-app` in `shpyrd-system` holds the keys `app-id`, `slug`, `private-key` (PEM), `webhook-secret`, `client-id`, `client-secret` and `html-url`.
- **shpyrd cloud:** the cloud repository writes this Secret for its one shared, public App. That is the whole contract with the cloud. The App's webhook points at the cloud's single cluster.
- **Self-hosted:** an operator registers the cluster's own App (below).

### Registering the App: `shpyrd-ctl` (operator, kubeconfig)

`shpyrd-ctl github app create [--org ORG] [--name NAME] [--public] [--url DASHBOARD_URL]`:

1. Reads the cluster's public dashboard URL from its install settings, the same source as `SHPYRD_DASHBOARD_URL`, unless `--url` is given.
2. Starts an HTTP server on `127.0.0.1:<random port>`. Its page auto-submits a form POST to `https://github.com/settings/apps/new` (or `https://github.com/organizations/<org>/settings/apps/new`) carrying the manifest and a random `state`. It then opens the browser on that page.
3. The manifest contains:
   - **Name:** `name` defaults to `shpyrd-<dashboard host>`, cut to GitHub's 34-character limit. `--name` overrides it.
   - **URLs:**
     - `url` is the dashboard URL;
     - `hook_attributes.url` is `<dashboard>/api/github/webhook`;
     - `redirect_url` is `http://127.0.0.1:<port>/created`;
     - `setup_url` and `callback_urls[0]` are `<dashboard>/api/github/installed`.
   - **Flags:** `request_oauth_on_install: true`, `setup_on_update: true`, and `public: <--public>`, default false.
   - **Permissions:** `contents: read`, `metadata: read`, `members: read`, `checks: write`, `deployments: write` and `statuses: read`. `members: read` lets shpyrd check that the person connecting an org is one of its owners.
   - **Events:** `push`, `installation_repositories`, `check_suite` and `status`. The `installation` events are always sent.
   - All of part 4's permissions are requested now, so installations never have to re-approve.
4. GitHub redirects to `127.0.0.1:<port>/created?code&state`. The CLI checks the state, calls `POST https://api.github.com/app-manifests/{code}/conversions` (no auth needed), writes the Secret through the kubeconfig, shows "Registered" in the browser, and prints the App's URL and the install link.
5. A timeout of 10 minutes; Ctrl-C stops the local server.

Also `shpyrd-ctl github app show` (slug, id, URL; never the keys) and `shpyrd-ctl github app delete` (removes the Secret, and says to delete the App on GitHub too).

The server has no registration endpoint. It reads the Secret when it needs it, cached briefly and reloaded when it changes.

### `pkg/github`

A small client with no new modules. `golang-jwt/jwt/v5` moves from indirect to direct.

- **The App's JWT:** RS256 from `private-key`, with `iat` set to now-60s, `exp` set to now+9min, and `iss` set to the app id.
- **Installation tokens:**
  - `InstallationToken(ctx, installationID, repo string)` calls `POST /app/installations/{id}/access_tokens` with `{"repositories":[name], "permissions":{"contents":"read"}}`.
  - Tokens are cached per installation and repository until 10 minutes before they expire.
  - A 404 or 422 for a repository the installation doesn't grant returns `ErrRepoNotGranted`.
- **Listings:**
  - `Repositories(ctx, installationID)` uses an installation token with no repository scope to page through `GET /installation/repositories`.
  - `Branches(ctx, installationID, owner, repo)`.
  - `DefaultBranch` comes from the repository.
- **OAuth:**
  - `ExchangeCode(ctx, code)` calls `POST https://github.com/login/oauth/access_token` with the client id and secret.
  - `UserInstallations(ctx, userToken)` calls `GET /user/installations`, with paging.
- **The base URL** can be configured (`SHPYRD_GITHUB_API_URL`, default `https://api.github.com`), so tests run against `httptest`.
- **Outbound safety:** calls go through the dial checks from PR 2's `pkg/gitauth`. They allow api.github.com and refuse loopback and metadata addresses, except under the test hook.

### Installations: the SQL store

- **Migration `000028_github_installations`:** `github_installations(id BIGINT PRIMARY KEY, workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, account_login TEXT NOT NULL, account_type TEXT NOT NULL, connected_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), suspended_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ)`.
- **A new `GitHub` sub-interface** on `Store`, with memory and postgres implementations:
  - `AddGitHubInstallation`: upsert by id. A re-connect to another workspace moves the installation and is audited.
  - `GitHubInstallations(workspace)`: not deleted.
  - `GitHubInstallation(id)`: across workspaces.
  - `SetGitHubInstallationState(id, suspended, deleted)`.
  - `DeleteGitHubInstallation(workspace, id)`: disconnect.
- **The single-workspace wrapper** (`pkg/store/one.go`) overrides `GitHubInstallation(id)` to return only the default workspace's installations, as `WorkspaceByHost` does.
- **Tests:** `dropAll` in store_test.go gains the table, and both implementations are tested.

### Connecting a workspace (workspace admin, `authz.ClusterAdmin`)

Who may connect, and the binding to the browser, were decided by the maintainer after the security review (2026-10-16):
- **Only an admin of the GitHub account may connect its installation.** For an Organization, `GET /user/memberships/orgs/{org}` with the person's user token must say `role: admin` and `state: active`. For a User account, `GET /user` must return that login. Seeing the installation is not enough: any member with read access to one repository sees it.
- **No silent move.** An installation another workspace has is refused with "<account> is connected to workspace <slug>: disconnect it there first." Re-connecting it to the same workspace is fine.
- **The flow finishes on the workspace's own address, with the session that started it.** The fixed callback checks the state, the code, the person's installations and that they administer the account. It then redirects the browser to `<workspace dashboard>/api/workspace/github/finish?ticket=…`. The ticket is signed, single-use and valid for 5 minutes. That route (ClusterAdmin) requires the dashboard session of the same identity as the state's actor before it stores the installation. A link started by someone else cannot finish in another person's browser.

- **Starting:** `POST /api/workspace/github/connect` returns `{installURL}`: `<html-url>/installations/new?state=<state>`.
- **The state:** base64url JSON `{ws, actor, exp}` (15 minutes) with an HMAC-SHA256 tag keyed by `client-secret`. It is compared in constant time.
- **The callback:** `GET /api/github/installed?installation_id&setup_action&code&state` is public, because GitHub sends the browser there.
  1. Check the state.
  2. `ExchangeCode`, then `UserInstallations`. The id must be in that list, otherwise refuse with a plain page.
  3. Discard the user token.
  4. Store the installation (account from the listing), audit `github.connect` with the workspace in the detail, and redirect to the workspace's Git settings page (`dashboardURLOf(ws)` plus the settings path).
  5. `setup_action=update` (repositories changed) just redirects.
  6. Errors render a server page (`pkg/pages` pattern) in plain words.
- **Reading and removing:**
  - `GET /api/workspace/github` returns `{configured bool, appURL, installations:[{id, account, type, connectedBy, createdAt, suspended}]}`.
  - `DELETE /api/workspace/github/installations/:id` disconnects, and audits `github.disconnect`.
- **For project developers** (`ProjectView` in the workspace):
  - `GET /api/workspace/github/repositories` lists `[{installation, fullName, private, defaultBranch}]`, merged across the workspace's installations and cached 60s.
  - `GET /api/workspace/github/repositories/:owner/:repo/branches` lists branches.

### Builds and the watcher

- **Lookup order:** `lookupGitBuild` (internal/controller/gitcreds.go) tries first, for an app whose URL host is `github.com`, when the App Secret exists and the workspace has a live installation whose `account_login` equals the repository owner (case-insensitive):
  - mint a repository-scoped token;
  - build a synthetic token Secret, username `x-access-token`, password the token;
  - call `gitauth.Resolve` as for a token, with `Build.Kind = gitauth.KindGitHubApp` (new) so the views can say so.
  - `ErrRepoNotGranted` or any mint error falls through to the deploy key, then the token, as before. A mint error is logged, and recorded in the watcher's record when the listing then fails.
- **Freshness:** a GitHub-App credential's `<app>-git` is rewritten when it is more than 50 minutes old (an annotation records the mint time). The AppReconciler requeues apps using it within 50 minutes, so kpack's own rebuilds find a live token.
- **The watcher** (PR 2) gets the same Build from `lookupGitBuild`, so `ls-remote` uses the installation token unchanged.
- **Views:** `git-auth show` and `ProjectGitAuthView` report `using: "github-app"` with the account. The Source block says "through the GitHub App (acme)".

### The App's webhook

`POST /api/github/webhook` is public:
- **Limits and checks:** a per-IP limit, a 5 MiB cap, then `X-Hub-Signature-256` against `webhook-secret` (from PR 2's gitauth). A missing App Secret answers 404, so the route only exists when an App does.
- **Events:**
  - **`push`:** look up the installation (`GitHubInstallation(id)`). If it is unknown, answer 202. Otherwise find the workspace's apps whose git URL names that repository (`repository.full_name`, compared case-insensitively on host and path) and whose branch the push names. Write PR 2's `git-push` annotation on each (sha, before, ref, message, author, delivery, skip). Dedupe on `X-GitHub-Delivery` as PR 2 does. Answer 202.
  - **`installation`:** `deleted`, `suspend` and `unsuspend` update the stored state and audit `github.disconnect`, `github.suspend` or `github.unsuspend`; `created` is ignored, because the setup callback does the connecting.
  - **`installation_repositories`:** nothing is stored, because the picker reads live.
  - **`ping`:** 200.
  - Anything else: 202.
- **Per-project hooks:** a project using the App needs no per-project hook. `shpyrd git-hooks show` says "Pushes arrive through the GitHub App (acme); nothing to set up."

### CLI: `shpyrd` (workspace admins and developers)

- `shpyrd github connect`: opens the install URL and polls `GET /api/workspace/github` until a new installation appears (10 minutes), then prints it.
- `shpyrd github list`.
- `shpyrd github disconnect <account|id>`.
- `shpyrd github repos [--filter TEXT]`.
- Signed in only, like `git-auth`.

### Dashboard (apps/workspace, built and reviewed through screenshots)

- **Workspace settings → Git** (a new page beside the others):
  - if no App is configured, a short note (on self-hosted: "an operator runs `shpyrd-ctl github app create`");
  - otherwise a **Connect GitHub** button (`POST …/connect`, then open the install URL);
  - a list of connections: account, type, connected by, suspended, **Manage on GitHub** (`<html-url>/installations/<id>` or the org's settings), and **Disconnect** with a confirm dialog.
  - Only workspace admins see the button and Disconnect.
- **New project:**
  - a **From GitHub** choice beside the existing git URL field, shown when installations exist;
  - an Input filter over a scrollable list of repositories (`@shpyrd/ui` only, no new library component);
  - a Select for the branch (the default branch first), then the existing name and exposure fields.
  - It creates the project with `git: {url: "https://github.com/<full_name>", revision: <branch>}`.
- **Source block:** "through the GitHub App (acme)" when used.
- **Plumbing:** types, backend, Mock with JSON fixtures, and Vitest tests named as sentences.

### Failures

- **The App Secret is missing:** the connect routes answer 409, "GitHub is not set up on this platform", with the operator hint on self-hosted. The webhook answers 404. Builds skip the App.
- **GitHub is down or rate-limited:** minting falls through to the other credentials. The picker shows the error. The callback page says to try again.
- **A suspended installation** is skipped by the lookup. The Source block shows the no-access reason from PR 1 and PR 2.
- **Audit:** `github.connect`, `github.disconnect`, `github.suspend`, `github.unsuspend` and `github.move`. The workspace goes in the target or detail, because these are cluster-ref entries.

## Testing

- **pkg/github:** a fake GitHub (`httptest`) for the JWT claims and signature, token minting and caching, `ErrRepoNotGranted`, paging, the OAuth exchange, and `/user/installations`.
- **pkg/store:** both backends; cross-workspace lookup; the single-workspace filter.
- **API:**
  - state signing and expiry;
  - the callback refuses an installation the person can't see;
  - connect, list and disconnect with authz;
  - the webhook's signature, push routing to apps (case, branch, unknown installation), installation events and dedupe.
- **Controller:**
  - the lookup prefers the App and falls through on `ErrRepoNotGranted`;
  - the token Secret is refreshed after 50 minutes;
  - the watcher lists with the App token.
- **CLI:** `shpyrd-ctl github app create` against a fake GitHub conversion endpoint, with the browser step simulated by posting to the local server; and `shpyrd github` against httptest.
- **Dashboard:** Vitest tests for the Git page and the From GitHub picker.
- **The real run on kind:**
  - register a real App on the maintainer's test org with `shpyrd-ctl`;
  - install it on a private test repository;
  - connect the workspace;
  - create the project from the picker;
  - push, and see it deploy;
  - suspend the installation, and see the build fail with the reason.
  - Screenshots go in the PR.
  - The webhook needs a public URL. The run uses a tunnel to the kind cluster, or GitHub's "redeliver" against a dashboard URL that GitHub can reach. The maintainer chooses.
