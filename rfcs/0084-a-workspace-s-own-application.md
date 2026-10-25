# RFC-0084 A workspace's own application: the door serves a chosen build

**Status:** implemented (the catalogue of publishers, tags and revocation is a later RFC)

**Owner:** Patrick Negri

**Depends on:** RFC-0080 (two doors; the applications served by host; `SHPYRD_UI_DIR`
and `SHPYRD_UI_IMAGE`), RFC-0033 (workspaces, their settings and branding), RFC-0008
(the Content-Security-Policy of the applications), RFC-0059 (the registry)

**Creation date:** 2026-10-24

**Last update:** 2026-10-25 (implemented)

---

## Summary

A workspace's door serves the workspace application: today the one built into the
server, the same for every workspace. This RFC lets a workspace choose an application of
its own: a build of the same kind (a folder of static files, what `next build` exports),
published to a registry of the customer's choosing (GHCR, Docker Hub, their own), named
by a reference in the workspace's settings, pulled with a credential the workspace holds.
The server fetches it, keeps it by its digest and serves it at that door by the rules the
platform's own is served by: same origin, same API, same policy. The platform's own
registry (RFC-0059) is not involved: it keeps holding the images of builds, nothing else. A workspace
without a reference gets the platform's own, which becomes one package among the others
rather than a special case. Nothing runs on the server for the customer; the API stays
the only thing their code talks to.

## Motivation

A customer may want the interface of their workspace to be theirs: their screens, their
words, their order of things, well past what a logo and a colour give (RFC-0033). An
agency that serves many customers wants one application of its own that every customer of
theirs runs, without the platform recording that those workspaces belong to the agency.
And the platform itself gains from shipping its interface apart from the server, as the
installer already can for a whole installation (`SHPYRD_UI_IMAGE`).

### Goals

- A workspace runs an application built outside the platform, by a reference in its
  settings, set by its administrators, with an audit line.
- One loader and one way of serving for every application, the platform's own included:
  no branch for "ours" and "theirs".
- The customer's code never runs on the server and never gains a power the platform's own
  interface does not have: it is static files, served from the workspace's origin, calling
  the same API with the same session.
- An application that cannot be fetched or is not an application never leaves a workspace
  without its door: the platform's own answers, and the failure is reported.
- Updating is changing the reference. Nothing is shared between workspaces but the cache.

### Non-Goals

- A catalogue of applications with publishers, channels and revocation (the "store"). It
  is the natural next step and is sketched under Alternatives; this RFC keeps the
  reference opaque so that step changes nothing here.
- Extension points inside the platform's own application (slots, remote modules).
- A build service that compiles the customer's repository. They build; we serve.
- The console. Its application stays the platform's own.
- Anything about who an agency is to the platform. The workspace knows a reference, not
  an author.

## Proposal

### What an application package is

The folder `next build` exports (`out/`): an `index.html`, the pages built for addresses,
`_next/` and the public files. Nothing else: no server code, no service worker beyond
what the platform's own ships, no file outside the folder. The server already knows how to
serve exactly this (RFC-0080): a file by its path, the page built for the address, the
entry for anything else, with the hashes of the scripts written into the pages in the
policy. The package may carry a `shpyrd-app.json` at its root naming it (`name`,
`version`) and declaring the origins beyond the server its pages load scripts from, connect
to or show images from; the policy admits those origins and nothing else, as it does for a
binary's page additions today.

### A reference names a source and a place in it

The workspace's settings carry `ui.ref`, a string. Its scheme says where the files come
from; what follows says which:

| Reference | Source | Use |
| --- | --- | --- |
| *(none)* | the platform's own | the default; the embedded applications, or `SHPYRD_UI_DIR` |
| `oci://<registry>/<repository>@sha256:<digest>` | an artifact or image in any registry | published applications; the only source on a cluster |
| `dir:/absolute/path` | a directory on the server's file system | development, tests, the example repository |

The package is pushed to any registry the server can reach, with the publisher's own
account there, by either of two shapes the server accepts alike:

- an **artifact**: one layer, a tar of the folder, as `oras push <ref> out/` makes;
- an **image** whose file system is the folder: `FROM scratch` + `COPY out/ /`, as
  `docker build` and `docker push` make, for publishers who have nothing but Docker.

The server takes the manifest's layers in order, as an image's file system, and serves the
folder with `index.html` at the root. The platform's registry is not a place to publish
to: it stays inside the cluster, for builds. A reference is always by digest in the
settings: a tag given to the API is resolved to a digest when it is set, so what a door
serves never changes behind the administrator's back. Following a tag is the catalogue's
job (Alternatives).

### Who sets it, and how

Workspace administrators, through the workspace API they already use for the name and the
branding: `PATCH /api/workspace {"ui": "<reference>"}` sets it, `{"ui": ""}` goes back to
the platform's own; `GET /api/workspace` shows `ui` when one is set. The CLI follows:
`shpyrd workspace ui set <reference>`, `shpyrd workspace ui clear`, `shpyrd workspace ui`.
Every change is an audit line (`workspace.update`, `ui: <reference>`). The settings page
of the workspace application shows the reference and offers the same two actions.

A private repository needs a credential to pull from. The workspace holds one per
registry host, the way it holds a token per Git host for private repositories (RFC-0017):
`PUT /api/workspace/registry-auth/<host> {"username", "token"}`, a read-only token the
publisher issues (a GHCR token with `read:packages`, a Docker Hub access token), kept as a
Secret of the system namespace labelled with the workspace, replaced or removed by the
same administrators, never shown back. A public repository needs none. Setting a reference
whose registry refuses the pull is refused with the registry's words, so a missing or
wrong credential is found when the reference is set, not when people arrive.

The platform operator chooses which sources a server accepts (`SHPYRD_UI_SOURCES`, default
`oci` on a cluster, `oci,dir` where the server runs for development) and may turn the
feature off (`off`): workspaces keep their reference but every door serves the platform's
own.

### Serving

At a workspace host the server resolves the tenant (as it does for sign-in and the API),
reads `ui.ref`, and serves that application; without one, or when it cannot be served, the
platform's own. A reference is fetched once per server replica and kept: in memory as the
loaded application (pages read, hashes computed, exactly as the platform's own is kept
today), and on disk by digest for OCI, under a cache directory beside the exported
applications, so a restart does not fetch again and a hundred workspaces on one digest
share one copy. A loaded application nobody references any more is dropped after a while.

A reference that fails (the registry does not answer, the digest does not match, the tar
has no `index.html`, the package is over the limit) is remembered as failed for a minute,
logged with the reason, counted (a metric per workspace), and the platform's own answers.
The workspace's settings page shows the last failure. The platform's own is loaded through
the same code: `uiFiles` gives the default package, `loadApplication` reads it, the door's
handler asks the cache for a reference or for the default. One path, three sources.

### Building one

The platform's `apps/workspace` is the template. A build of one's own needs the design
system (`@shpyrd/ui`, on npm) and the shared code (`apps/shared`, to be published as
`@shpyrd/shared` so a fork needs none of the core). Until then the example repository
`shpyrd-example-workspace-app` vendors both as copies, as `shpyrd-signup` does. The
workflow is `npm run build`, then push `out/` as an artifact, then set the reference.
The API the application talks to is the server's, same origin: no CORS, no token in the
page, the session cookie as today.

### User Stories

- **A customer with an interface of their own.** They fork the example, change the
  launcher and the project pages, build, push to their workspace's registry, set the
  reference. Their people see their application at their door the next request.
  Updating is pushing again and setting the new digest; going back is setting the old one
  or clearing it.
- **An agency.** They maintain one application and publish it to their own registry
  (`ghcr.io/<agency>/workspace-ui`), public or private. When they create a customer's
  workspace they set the reference, and the read token when the repository is private.
  The workspace records the reference, the token and who set them, nothing about the
  agency: the token is one the agency may revoke, and the package stays in the agency's
  registry until a workspace installs it.
  Moving the customers to a new version is one `PATCH` per workspace today; the catalogue
  (Alternatives) gives them a tag to move instead.
- **The platform.** The platform's own application is a package like the others, which
  the installer may point at an image (`SHPYRD_UI_IMAGE`) today and at an OCI reference
  tomorrow: an interface release without a server release.

### Alternatives

Ten shapes were weighed (2026-10-23). In short:

1. **Branding by settings** (logo, colour, texts): already there for the logo and the
   colour; it stays, under any application, since it is API data.
2. **HTML injected per workspace** (the page additions a binary may make, made a
   workspace setting): cheap, but it is a script of a third party running in every page
   under our versions; good for analytics, not a base for an interface of one's own.
3. **A built application served in place of the platform's own.** This RFC.
4. **A fork deployed as a project of the platform**, the door pointing at it: works today
   with cookies across hosts; hosting, not a choice of the workspace.
5. **A shell with extension points** (remote modules per route or slot): the most
   powerful, the most expensive; a contract to keep. Later, if ever.
6. **A catalogue of packages**: 3 with a publisher identity, versions, channels and
   revocation. The next RFC; this one keeps the reference opaque so it adds a layer
   rather than replacing one.
7. **A declarative manifest** of what the platform's own shows: no code, always
   compatible, limited to what it expresses. Can live beside 3.
8. **A managed build** of the customer's repository: 6 with the build on our side. Only
   once agencies prove to stall on `next build`.
9. **Headless**: the API and the SDK alone. True regardless; documented, not built.
10. **An installation of their own** for large agencies: the licence (ee) already allows
    it; not a per-workspace matter.

3 was chosen because the server already serves a folder it did not build (RFC-0080) and
because it is the mechanism 6 and 8 need anyway. 1 stays as it is. 2 is kept out of the
product.

## Design Details

### Settings and API

```go
// store.WorkspaceSettings
UI *WorkspaceUI `json:"ui,omitempty"` // nil: the platform's own

type WorkspaceUI struct {
    Ref   string    `json:"ref"`             // oci://... or dir:/...
    SetBy string    `json:"setBy,omitempty"` // who set it (email or token name)
    SetAt time.Time `json:"setAt,omitempty"`
}
```

`PATCH /api/workspace` takes `ui` (a string; `""` clears). The server checks the scheme
against `SHPYRD_UI_SOURCES`, requires an absolute path for `dir:`, resolves a tag to a
digest for `oci:` (refusing when the registry cannot be reached), and refuses an artifact
over the limit before storing the reference. `WorkspaceView.ui` is the reference as
stored. The audit line names the reference, or `(default)`.

### Package limits

- At most 64 MB extracted, at most 4000 files, paths without `..`, regular files only.
- HTML files are the pages; everything else is served as a file by its path with the
  content type of its extension.
- `shpyrd-app.json`: `{ "name": "...", "version": "...", "origins": ["https://..."] }`;
  `origins` are checked as the page-additions origins are (a scheme and a host, no path).
  Absent, the package gets the base policy, as the platform's own does.

### Fetching an OCI artifact

The reference's digest is the manifest's. The server pulls the manifest, takes the single
layer, streams it to the cache directory while hashing, refuses a mismatch or an oversize
tar, extracts into `<cache>/<digest>/` and only then makes the directory visible to the
loader. With more than one layer (an image), the layers are applied in order and
whiteouts honoured, as a runtime would. Credentials: the workspace's for that registry
host when it holds one, anonymous otherwise; the platform's own registry credentials are
never used, since the platform's registry is not a source. A pull runs once per digest
per replica; concurrent requests for the same digest wait for it.
Cache directory: `SHPYRD_UI_CACHE_DIR`, default `<SHPYRD_UI_DIR>/.cache` when the
applications come from a directory, `<SHPYRD_DATA_DIR>/ui` otherwise. Entries unused for
30 days are removed.

### One path

`pkg/server/ui.go` keeps giving the default package (the embedded applications or
`SHPYRD_UI_DIR`). `pkg/api/ui.go`'s loader stays as it is. Between them, a package cache
keyed by reference answers `get(ref)` with a loaded application or nil, with the default
registered under the empty reference. The handler at a workspace host asks for the
tenant's reference and falls back to the default. The console keeps its own folder.

### Development

`dir:` points at a build on the developer's machine; the server is told
`SHPYRD_UI_SOURCES=oci,dir`. A rebuilt directory is picked up after the loaded copy is
dropped (`POST /api/workspace/ui/reload`, admin; or a restart). Tests load applications
from `fstest.MapFS` and temporary directories as the RFC-0080 tests do.

### Security

What does not change: the application runs in the browser with the user's session, as the
platform's own does, and every API call is checked by the server for that session and its
roles. The code gains nothing by being someone else's.

What changes: the administrators of a workspace choose code that runs with their people's
sessions. This is inherent to an interface of one's own, in every shape above that
involves code. The RFC bounds it:

- **Same origin and the same policy.** The package is served from the workspace's host by
  this server. The base policy forbids scripts, connections and images from anywhere else;
  a package that needs an origin declares it, and the declaration is visible where the
  reference is shown. There is no quiet way out of the page.
- **Static only, bounded.** No server code, no file outside the folder, size and count
  limits, a digest that must match. The server never executes anything from a package.
- **Administrators only, audited.** Setting a reference is a `ClusterAdmin` action of the
  workspace, recorded with who and when, shown on the settings page.
- **Sessions do not move.** The API is the same origin; the cookie is `HttpOnly` as
  today; no token is written into the page; no CORS is opened.
- **Fallback, never a blank door.** Any failure serves the platform's own, so a broken or
  removed package cannot lock people out, and an administrator can always clear the
  reference from the platform's own settings page (at `/workspace`, which is also where
  the chosen application would show it; the platform's own is reachable by clearing
  through the CLI when the chosen one hides it).
- **Later, with the catalogue:** signed artifacts, a publisher identity, revocation of a
  publisher, a reviewed channel.

### Compatibility of the API

Today the application and the server ship together; a workspace's own application may be
built against an older API. The server exposes its version and the API's in
`/api/config` (it does in part today); the platform keeps a route's shape for two minor
versions after announcing a change in the release notes, and additive changes need no
notice. This is the real cost of the RFC and it falls on the platform, not on the
customer.

### Operations

- **Is it in use?** `GET /api/workspace` shows `ui`; the console's workspace list (cloud)
  gains a column; a gauge counts loaded packages and a counter the failures, by workspace.
- **Enabling and disabling:** `SHPYRD_UI_SOURCES` (`off` disables; references are kept).
  Default on a cluster is `oci`.
- **Drawbacks:** memory per loaded package (the pages of a build, a few MB each);
  one pull per digest per replica; the API compatibility promise above.

## Decisions on the open questions (2026-10-25)

- **The default package on a cluster** stays the init container's `SHPYRD_UI_DIR`; an
  OCI reference for the platform's own is a follow-up.
- **Who may set the reference:** `ClusterAdmin` of the workspace, like the branding. A
  separate permission for the agency case waits for the catalogue.
- **`/api/config` carries the reference** as `workspace.ui`.
- **`@shpyrd/shared` on npm** is still to do; the example repository vendors it and
  `@shpyrd/ui` until then.
- **The platform's own registry never hosts packages.** Publishers use a registry of
  their own; the platform's stays for builds, unchanged, not swapped for a provider's.
- **The registry credential** is a Secret of the system namespace per workspace and
  host, as Git tokens are (RFC-0017).

## Implementation History

- 2026-10-24: proof of concept on the branch of this RFC: `ui.ref` in the settings,
  `PATCH /api/workspace {"ui"}`, the `dir:` source, the per-reference cache with the
  one-minute retry, the fallback to the platform's own, tests in `pkg/api/ui_test.go`;
  the example repository `shpyrd-example-workspace-app` (the platform's `apps/workspace`
  with Acme's title, accent and a note on the launcher, `@shpyrd/ui` and `apps/shared`
  vendored) built and served at a local door. Not in the PoC: `oci:`, `shpyrd-app.json`
  and its origins, limits, the CLI, the settings page, `SHPYRD_UI_SOURCES`, metrics.
- 2026-10-25: decided that packages live in a registry of the publisher's (GHCR or any
  other), pulled with a read credential the workspace holds per registry host; the
  platform's registry is neither a source nor changed.
- 2026-10-25: implemented. `pkg/uipkg` parses references, settles tags, pulls artifacts
  and images (an index's linux/amd64 manifest; layers in order with whiteouts; the
  digest verified while reading) into a cache by digest under `SHPYRD_UI_CACHE_DIR`,
  with the limits above and `shpyrd-app.json`; `pkg/api` serves the chosen application
  with its origins in the policy, keeps a status per reference (serving, failed with the
  reason, pending, disabled), counts failures (`shpyrd_workspace_ui_failures_total`) and
  loaded packages (`shpyrd_workspace_ui_loaded`), refuses a reference that cannot be
  pulled when it is set, and holds registry credentials per host
  (`/api/workspace/registry-auth/<host>`); `POST /api/workspace/ui/reload`;
  `SHPYRD_UI_SOURCES`; `shpyrd workspace ui` and `shpyrd registry-auth`; the page
  Workspace › Application; the docs under Dashboard. Checked end to end against a
  registry behind basic auth on this machine: refused without the credential, served
  with it, the platform's own again when cleared. Left for later: an OCI reference for
  the platform's own package, `@shpyrd/shared` on npm, the catalogue (RFC to come).
