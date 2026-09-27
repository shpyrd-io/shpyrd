# RFC-0067 Build profiles: automatic buildpack configuration

**Status:** implemented

**Owner:** Patrick Negri

**Depends on:** RFC-0004 (implemented), RFC-0065 (implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

The CLI inspects the source directory before deploying and applies a *build profile*: a
named set of buildpack environment variables and `shpyrd.yaml` defaults that make an app
work without any configuration. A `public/index.html` file means `BP_WEB_SERVER=nginx` and
`BP_WEB_SERVER_ROOT=public`; a Vite `package.json` with no `start` script means
`build.buildpacks: [web-servers]`, `BP_NODE_RUN_SCRIPTS=build`,
`BP_WEB_SERVER=nginx`, `BP_WEB_SERVER_ROOT=dist`. The CLI prints what it detected and
why, so nothing is invisible.

`shpyrd projects create` gains a `--save` flag (and a `--save` prompt when a `shpyrd.yaml`
does not already exist) that writes the detected profile into a `shpyrd.yaml` in the
current directory, ready to commit.

## Motivation

Every example in `shpyrd-io/shpyrd-examples` needed hand-tuned `shpyrd.yaml` settings that
are mechanical:

- A static site with `public/index.html` always needs `BP_WEB_SERVER` and
  `BP_WEB_SERVER_ROOT`. Without them the Paketo web-servers buildpack does not detect and
  the build fails with "No buildpack groups passed detection". That error message does not
  help.
- A React/Vite app has no `start` script in `package.json`, so the Node buildpack wins
  detection and builds an image with no process to start (`exit 82: failed to launch:
  determine start command`). The fix — `build.buildpacks: [web-servers]` plus four
  `BP_*` vars — is the same for every Vite project.
- `RACK_ENV=production` is always required for Sinatra 4 and every other Rack app that
  respects it, because the framework's host-authorization middleware only permits real
  hostnames in production mode.
- A Ruby app needing system packages needs `stack: full` for any package with deep
  transitive dependencies (glib, libcurl, …), yet that choice is invisible until the app
  crashes at run time.

The detection failures and crashes are all avoidable if the CLI reasons about the source
directory the way a developer would.

### Goals

- No configuration for the common cases: `shpyrd deploy` in a Rails app, a Vite project,
  or a directory with `public/index.html` works out of the box.
- Every inference is printed before the build starts: "Detected: static site on nginx
  (public/index.html found). Add `shpyrd.yaml` to customise."
- `shpyrd projects create --save` (and `shpyrd deploy --save`) writes the inferred
  `shpyrd.yaml` so the developer can inspect and commit it.
- The developer can always override: an explicit `shpyrd.yaml` wins over every inferred
  value; an inferred value wins over the buildpack's own detection.

### Non-Goals

- Replacing buildpack detection entirely (the platform still uses buildpacks for language
  identification; profiles only provide the missing `BP_*` hints).
- Detecting every possible framework (start with the highest-ROI cases; add more over
  time).
- Changing what the server stores: profiles are a CLI concern, resolved before the deploy
  request is sent.

## Proposal

### Profiles

A profile is a named rule with:

1. A **predicate** — a set of file or content checks that identify the pattern.
2. A set of **inferences** — build env vars, `buildpacks`, `stack`, and process-level env
   vars to apply when no explicit `shpyrd.yaml` value covers them.
3. A **message** logged before the build: "Detected: X (reason). ..."

Profiles are checked in priority order; the first that matches wins. An explicit
`shpyrd.yaml` field always takes priority over any inferred value.

#### Initial catalog

| Name | Predicate | Inferences |
| --- | --- | --- |
| `static-nginx` | `public/index.html` exists **and** no `package.json` | `BP_WEB_SERVER=nginx`, `BP_WEB_SERVER_ROOT=public`, `build.buildpacks: [web-servers]` |
| `static-httpd` | same as above **and** `shpyrd.yaml` says `BP_WEB_SERVER=httpd` | `BP_WEB_SERVER_ROOT=public`, `build.buildpacks: [web-servers]` |
| `react-vite` | `package.json` with a `build` script **and** `vite` in `devDependencies` **and** no `start` script | `build.buildpacks: [web-servers]`, `BP_NODE_RUN_SCRIPTS=build`, `BP_WEB_SERVER=nginx`, `BP_WEB_SERVER_ROOT=dist`, `BP_WEB_SERVER_ENABLE_PUSH_STATE=true` |
| `next` | `package.json` with `next` in `dependencies` | `BP_NODE_RUN_SCRIPTS=build`, `NODE_ENV=production` (build env) |
| `rack` | `config.ru` exists | `env.RACK_ENV=production` |
| `rails` | `config/application.rb` exists (implies Rack) | `env.RACK_ENV=production`, `env.RAILS_LOG_TO_STDOUT=1`, health check path `/up` for web |
| `ruby-apt-full` | `Aptfile` exists **and** any listed package has a known deep dependency tree (libvips42, libmagick, imagemagick) | `build.stack: full` |

The `static-httpd` profile only fires when the developer has already written `BP_WEB_SERVER=httpd`; without it `static-nginx` fires and nginx is used. This matches the "opinionated default with explicit override" principle.

### CLI changes

#### `shpyrd deploy`

Before calling `deployRequest`, the CLI runs `detectProfile(dir, projectConfig)`:

1. Walk the source directory (the same tree `archiveSource` would tar).
2. Evaluate predicates in priority order against the files found.
3. For each matched profile, merge its inferences into the project config, printing a
   single line per inference that came from a profile rather than from `shpyrd.yaml`:

```
==> Detected: static site on nginx (public/index.html)
    Inferred BP_WEB_SERVER=nginx, BP_WEB_SERVER_ROOT=public (add to shpyrd.yaml to silence this)
```

4. If the profile adds a `build.buildpacks` composition and the deploy request does not
   already carry one, the CLI sets it (this is the React/Vite fix — uses the web-servers
   buildpack rather than the default platform builder).

#### `shpyrd projects create --save` / `shpyrd deploy --save`

`--save` writes a `shpyrd.yaml` with the inferred values into the current directory before
deploying. If `shpyrd.yaml` already exists, it is updated with the missing inferred fields
(a merge, not a replace).

`shpyrd projects create` already has a `--save` flag that writes only `project: <slug>`.
After this RFC it writes the full inferred profile too.

A prompt is shown on `shpyrd deploy` in an interactive terminal when no `shpyrd.yaml`
exists and a profile was detected:

```
==> Detected: static site on nginx (public/index.html)
    Save these settings to shpyrd.yaml? [y/N]
```

Answering yes writes the file; the deploy proceeds either way.

#### `shpyrd projects create` (the `--save` flag already exists)

```sh
shpyrd projects create "My Static Site" --public --save
```

Today writes only `project: my-static-site`. After this RFC, when run inside a directory
with a detectable pattern, it also writes the inferred `build.buildpacks`, `build.env`,
and `env` fields. If there is nothing to infer, behaviour is unchanged.

### Server / API

No server change. Profiles resolve to `shpyrd.yaml` fields that the CLI already sends in
the deploy request. The API does not know about profiles.

## Design Details

- **Priority**: explicit `shpyrd.yaml` wins over profile; profile wins over buildpack
  detection. If both a static-nginx profile and a `build.env.BP_WEB_SERVER=httpd` are
  present (via `shpyrd.yaml`), the `httpd` value wins.
- **Composition and profiles**: `react-vite` sets `build.buildpacks: [web-servers]`. This
  is the same composition the operator writes explicitly; the profile just does it
  automatically. The server validates it as it would any explicit request.
- **`--save` is not `--working-tree`**: `--save` writes the inferred settings to disk
  before archiving; it does not change whether the archive comes from `git archive HEAD`
  or `tarDirectory`. A developer commits the written file on their own.
- **Unknown buildpack in profile**: the `web-servers` buildpack must be in the catalog. If
  the operator has removed it, `detectProfile` skips the inferred `build.buildpacks` and
  emits a warning. The CLI already validates buildpack names before sending.
- **Interactive detection**: file-reading happens on the local directory (after
  `loadProjectConfig`), not on the server. For `--git` and `--image` deploys there is no
  local directory to inspect; profiles are skipped and a hint is printed if one would have
  fired for the repository name's default branch (`git clone` is not performed just to
  detect).
- **Profile for the `revision` example**: already works without a profile (the Go
  buildpack detects `go.mod`). Not a profile candidate.

## Implementation status

Implemented in v0.9.10 (`internal/cli/profiles.go`, `projectconfig_save.go`). Profiles:
static site (`public/index.html`, no language files), Vite single-page app (`vite` in
`package.json`, a `build` script, no `start`), Next.js, Rack (`config.ru`), Rails
(`config/application.rb`; the `/up` health check only when `config/routes.rb` routes
`rails/health#show`; `RAILS_ENV` rather than `RACK_ENV`, which is what Rails reads), PHP
(`public/index.php`), and an Aptfile add-on that picks the `full` stack for packages with
deep dependency trees (libvips, ImageMagick, ffmpeg, GDAL, OpenCV, Tesseract, Chromium,
wkhtmltopdf, LibreOffice, Poppler, Graphviz). A Dockerfile build takes the runtime
variables but no buildpack hints. `shpyrd deploy --save` and `shpyrd projects create
--save` write the inferred values with the file's comments and order kept
(`sigs.k8s.io/yaml/goyaml.v3` nodes). Verified against the examples repository: the
static, React, Sinatra, Rails, PHP, Next.js and Aptfile examples deploy from a
`shpyrd.yaml` that carries only the project and a size.

Known gaps, deliberately: no interactive "save these?" prompt on `shpyrd deploy` (the
`--save` flag is explicit; a prompt in a deploy that people also run from scripts felt
wrong — open question 6 answered "flag only"); `--git` deploys inspect nothing (no clone
just to detect); a root `index.html` without `public/` and Create React App (`build/`)
are not recognised; the `static-httpd` variant is only the explicit `BP_WEB_SERVER=httpd`
winning over the inferred nginx, as designed.

## Open questions

1. **What to do when `--save` is given but `shpyrd.yaml` already has every field the
   profile would write?** Default: silently do nothing (the file is already complete).

2. **Should the `rack` profile apply to non-Sinatra apps (Grape, Hanami, plain Rack)?**
   Default: yes — `RACK_ENV=production` is Rack convention, not Sinatra-specific.

3. **PHP**: `public/index.php` with `BP_PHP_WEB_DIR=public` and `BP_PHP_SERVER=nginx` is
   a strong enough signal for a profile. Default: add it.

4. **`react-vite` root detection**: `vite` in `devDependencies` may miss Vite listed
   under `dependencies` (unusual). Check both. Default: yes.

5. **Should inferred env vars (like `RACK_ENV`) go into `shpyrd.yaml`'s `env:` key or
   into `build.env`?** Runtime env vars (`RACK_ENV`, `RAILS_LOG_TO_STDOUT`) belong in
   `env:`; build-time vars (`BP_*`, `NODE_ENV`) belong in `build.env`. Default: as stated.

6. **How verbose should the inference output be?** Default: one line for the profile name
   and one for the inferred fields (suppress if `shpyrd.yaml` already covers them all).

## Implementation History

- 2026-09-27: RFC written.
- 2026-09-27: implemented (v0.9.10); the examples repository leans on it.
