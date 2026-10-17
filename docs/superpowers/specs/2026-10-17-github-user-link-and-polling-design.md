# Private repositories: GitHub access by person, and webhooks before polling

Date: 2026-10-17
Status: draft, awaiting review
Builds on: `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md` and the
three specs after it (the watcher, Connect GitHub, checks and Deployments). The work lands
on PR #223, which now holds the whole private-repositories feature.

Where this file and the earlier specs differ, this file wins.

## Why

The maintainer reviewed #223 and decided two things:

1. **Who can pick what follows the person, as on Vercel.** Today any member of a workspace
   can pick any repository the workspace's GitHub installation covers, even one they cannot
   open on GitHub. On Vercel, each person links their own GitHub account, and the import
   screen shows only the repositories that person can read. Builds, webhooks, checks and
   Deployments still go through the installation, so a person who leaves breaks nothing.
   "Follow Vercel's lead."
2. **No polling by project at scale.** shpyrd may host thousands of projects, and a check
   every 2 minutes for each one is too much traffic. Vercel, Render, Netlify and Railway don't
   poll at all; they rely on webhooks. GitHub follows Vercel: webhooks, plus a cheap catch-up
   for deliveries that failed. Other hosts keep a bounded poll until their webhook has worked
   once (option (a) in the conversation).

Both must work the same on the open-source core and on shpyrd cloud. The cloud's account
system is separate and is not involved.

## Done when

- A person who opens New project → From GitHub without a linked GitHub account sees
  **Continue with GitHub**. One click links it in a popup, and the dialog keeps what was
  typed.
- The picker lists only the repositories that the person's own GitHub account can read,
  out of those the workspace's installations cover. It has an account switcher, and
  **Add GitHub account or organisation** at its bottom.
- Creating a project from a GitHub repository, or changing a project's repository to one,
  is refused unless the person (or, for an API token, the person who made it) can read
  that repository on GitHub. This holds in the dashboard, the CLI and for agents.
- The person menu shows **GitHub: <login>** with Unlink, or **Link GitHub**. The CLI has
  `shpyrd github link`, `shpyrd github unlink` and `shpyrd whoami`, which shows the login.
- When the person who made a project leaves the workspace, or unlinks, the project keeps
  deploying.
- A repository covered by the GitHub App is never polled. A missed GitHub webhook is
  redelivered by a sweep within about 5 minutes.
- A repository on another host is polled at most every 5 minutes, once for all the projects
  on it, under a cluster-wide cap. It stops being polled once its webhook has delivered.
- A real run on kind against the test App shows each of these, with screenshots.

Out of scope:
- Vercel Pro's check on commit authors (a push from a GitHub user who isn't a member waits for
  approval). It's a candidate for later.
- Linking automatically on GitHub sign-in.
- GitLab and Bitbucket integrations.

## Part A: GitHub access by person

### What is stored

A new SQL table in core, migration `000029_github_user_links`, with one row per shpyrd
identity:

| Column | |
|---|---|
| `provider`, `subject` | the shpyrd identity: the same pair the Connect ticket is bound to. On core it comes from Dex; on cloud, from the cloud's OIDC. Primary key. |
| `github_user_id`, `github_login` | who they are on GitHub |
| `refresh_token` | encrypted, see below |
| `refresh_expires_at`, `linked_at`, `updated_at` | |

**A deliberate change from what was said in chat: the refresh token is stored, as Vercel
does.** Agents and the CLI must be able to create a project from a private repository
without a browser in the loop. That is the "share through your agent" focus. An
interactive-only token can't serve them.

The token is safer than the OAuth tokens behind Heroku's 2022 incident:
- **Limited to the App's permissions:** a GitHub App user token can do only what the App may
  do (contents read, metadata, checks and Deployments) **and** what the person may do.
  There's no repo-wide write scope and no account access.
- **Short-lived:** the access token lasts 8 hours and the refresh token 6 months. Both are
  rotated on every refresh.
- **Encrypted at rest:**
  - AES-256-GCM, with a key in a cluster Secret `shpyrd-token-key` (32 random bytes, made by
    the server when absent, never logged).
  - The access token is kept in memory only, never stored.
  - Unlinking revokes the token at GitHub (`DELETE /applications/{client_id}/token`) and
    deletes the row.

The App already asks for user authorization during installation. Its manifest needs
`request_oauth_on_install: true` (already set) and expiring user tokens (GitHub's
default for new Apps). The test App is checked for both during the real run.

### Linking

- **The flow:**
  1. `POST /api/me/github/link` returns GitHub's authorize URL with a signed, single-use state
     (15 minutes) bound to the person's provider and subject, plus a `return` path.
  2. The dashboard opens it in a **popup**, so the New project dialog keeps its fields.
  3. GitHub returns to the App's registered callback (`/api/github/installed`, as today).
     There, the state marks it as a link rather than a connect.
  4. The server exchanges the code, reads `GET /user`, and stores the link. The account
     belongs to whoever authorized it; no ownership check is needed.
  5. The page posts `{github: "linked"}` to its opener and closes.
- **The state** is checked like Connect's: its signature, its expiry, single use, and the
  same provider and subject on finish. A link started by someone else can't land on your
  identity.
- **Reading it:** `GET /api/me/github` returns the login, or 404. `DELETE /api/me/github`
  unlinks.
- **CLI:** `shpyrd github link` opens the browser to a page that starts the same flow, then
  polls `GET /api/me/github` until it's done. `shpyrd github unlink` and `shpyrd whoami` read
  and delete it.
- **An API token** acts as the person who made it. Its GitHub link is that person's link.
- **Audit:** `github.link`, `github.unlink`.

### The picker

- **Accounts:** `GET /api/github/accounts` lists the workspace's installations that the
  person can see, using `GET /user/installations` with the person's token, intersected with
  the workspace's installations. It is the account switcher.
- **Repositories:** `GET /api/github/repositories?installation=<id>` lists
  `GET /user/installations/{id}/repositories` with the person's token. That is only what
  they can read. It is paged and cached for 60 seconds per person.
- **Without a link,** both answer `409 {"code":"github_link_required"}`, and the dialog
  shows **Continue with GitHub**.
- **Below the list:**
  - **Can't find a repository?** links to the installation's settings on GitHub, where repos
    are added;
  - **Add GitHub account or organisation** opens the App's install page (Connect's existing
    flow).
- **Branches** are read with the person's token too.

### The check on create and on change

`POST /api/projects` with a github.com source, and any request that changes a project's
`git.url` (PATCH, deploy), verifies that the person can read the repository:
- **The call:** `GET /repos/{owner}/{repo}` with the person's token. A 200 that names an
  installation the workspace has passes.
- **Refusals:**
  - no link: 409 `github_link_required`;
  - a 404 or 403: 403 "You can't read <owner>/<repo> on GitHub";
  - a repository outside the workspace's installations: 422, as today.
- **Exemptions:**
  - a public repository needs no link, as on Vercel;
  - a redeploy of the same URL, or a change to anything other than the URL, doesn't check.
- **Recorded:** the project keeps `shpyrd.io/github-linked-by` (the GitHub login) for the
  audit trail and the dashboard ("Connected by paezao"). Nothing depends on it.

### Connecting installations

This stays as it is: only an owner of the GitHub account can attach an existing
installation to a workspace, and an installation belongs to one workspace. The
**Add GitHub account or organisation** path is the same Connect flow, now started from
the picker as well as from Workspace settings → Git.

### Unchanged

- Builds, webhooks, the watcher, the reporter, the scopes of installation tokens, and the
  one-workspace rule.
- A person leaving changes nothing for their projects.

## Part B: webhooks before polling

### GitHub App repositories: no polling by project

- **Polling:** the source watcher no longer polls a repository the App covers. It still reads
  the branch once when a project is created, when a person deploys, and when the branch or
  the credential changes.
- **The catch-up sweep:** a new loop in the controller manager, `github_sweep.go`. Every 5
  minutes, and once at start:
  - It reads `GET /app/hook/deliveries` with the App's JWT, newest first, back to the last
    sweep. At start it looks back 24 hours; GitHub keeps 3 days.
  - It redelivers, through `POST /app/hook/deliveries/{id}/attempts`, every delivery for
    `push`, `check_suite`, `status` or `installation*` whose last attempt failed (status
    0 or ≥ 400) and that has no later successful attempt of the same `guid`.
  - At most 50 per sweep, oldest first. Ids are remembered for a day so nothing is resent
    twice.
  - It runs on the leader only. The cost is one or two REST calls every 5 minutes for the
    whole cluster, however many projects there are.
- **Safe to redeliver:** the webhook handler already ignores repeated delivery ids, and pushes
  that don't follow are checked against the branch.
- **A delivery GitHub never sent** is not caught, just as on Vercel. A push or a Redeploy
  fixes it.

### Other hosts: poll until the webhook works

- **Per repository, not per project:** projects are grouped by (normalised URL, credential).
  One `ls-remote` answers every project and branch in the group.
- **A cluster-wide cap:** at most `SHPYRD_GIT_POLL_RATE` listings a second (default 5), from a
  token bucket, with jitter spreading the checks over the interval.
- **Interval:** `SHPYRD_GIT_POLL_INTERVAL`, default **5 minutes** (was 2), minimum 30 seconds.
- **Stop once the webhook delivers:** once a project's generic webhook has delivered once
  (`LastWebhookAt` set), that project is no longer polled. The Source block says "Deploys on
  push through its webhook (last delivery 3 days ago)". While polling, it says "Checked every
  5 min. Set up the webhook to deploy at once and stop the checks."
- **The existing backoff on failures stays.**

### Wait for CI

- **The fallback recheck of a held commit goes from 30 seconds to 2 minutes.** The
  `check_suite` and `status` webhooks, and their redelivery by the sweep, are the real path.
- **The shared CI read stays,** and its window becomes 90 seconds. The one-hour limit is
  unchanged.

### Removed

- The 24-hour "webhook recent" rule and the hourly polling of GitHub repositories.
- The 30-second CI recheck.

## API and CLI summary

| | |
|---|---|
| New | `POST/GET/DELETE /api/me/github`, `/api/me/github/link`, `GET /api/github/accounts` |
| Changed | `GET /api/github/repositories` (per person, `installation=`), `GET /api/github/branches` (person's token), the project create, PATCH and deploy checks |
| CLI | `shpyrd github link`, `unlink`; `shpyrd whoami` shows the GitHub login; a `github_link_required` error says "Run shpyrd github link" |
| Dashboard | the picker's link step, account switcher, "Can't find a repository?"; the person menu's GitHub item; the Source block's wording for polling |
| Docs | deploying.mdx (linking your GitHub account, who can pick what, the catch-up, polling for other hosts), cli.mdx |

## Testing

- **Unit:**
  - the encryption round trip and a wrong key;
  - the link state (signature, expiry, single use, identity);
  - the sweep's selection: failed, then later succeeded, then already resent;
  - grouping repositories for polling;
  - the token bucket.
- **The fake GitHub** gains:
  - user tokens and refresh tokens;
  - `/user`, `/user/installations`, `/user/installations/{id}/repositories`, and `/repos/{o}/{r}` with a user token;
  - revocation;
  - `/app/hook/deliveries` and redelivery.
- **API:**
  - linking end to end;
  - the picker shows only the person's repositories;
  - create is refused for a repository the person can't read, refused without a link, and allowed for a public one;
  - an API token uses its maker's link;
  - unlink revokes.
- **Controller:**
  - a covered repository is never listed on a timer;
  - another host is listed once for three projects, then not at all after a webhook;
  - the cap holds with a thousand projects;
  - the sweep resends a failed push, and the project deploys.
- **Real run on kind** with the test App:
  - link from the dialog;
  - the picker shows only paezao's repositories;
  - a second test person without access sees none and is refused on create;
  - stop the tunnel, push, start it, and the sweep redelivers and the project deploys;
  - a project from a public non-GitHub repository is polled and then stops after its webhook.
  - Screenshots go onto #223.

## Risks

- **A stolen database still needs the key.** It's in a separate Secret. Rotating the key
  means relinking; the docs say so.
- **The App's user authorization must allow expiring tokens.** An App made before GitHub
  defaulted to them may have them off, and the real run checks this.
- **Refresh tokens expire after 6 months unused.** The person sees **Continue with GitHub**
  again; nothing else breaks.
