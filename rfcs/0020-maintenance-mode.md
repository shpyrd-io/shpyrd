# RFC-0020 Maintenance mode

**Status:** implemented

**Owner:** Patrick Negri (shpyrd-io/shpyrd main)

**Depends on:** RFC-0001 (implemented), RFC-0081 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-28

## Summary

`shpyrd maintenance on` serves a branded "under maintenance" page (HTTP 503, `Retry-After`) at
every address of the project while its processes keep running; `off` restores traffic. A
message of the team's own goes on the page. Nobody is let through the page, the team included:
the team reaches the app with `shpyrd shell` and `shpyrd run`, and releases, scaling and
config changes go on underneath, as on Heroku.

## Motivation

Migrations and incidents need a way to take an app offline for visitors without touching the
deployment, and to show them something better than an error page or a login redirect.

### Goals

- One command or switch; instant; reversible; visible in the header, the project list, the
  launcher and the audit trail.
- Search engines and webhooks see a 503 with `Retry-After`, not an error page, and no sign-in
  flow starts.
- The processes keep running; releases, scaling, shells and one-off commands work while the
  page is up, so the work the page is for can be done behind it.

### Non-Goals

- Non-HTTP processes (they are unaffected).
- Letting anyone through the page. The first text of this RFC proposed an IP allowlist "so the
  team can still reach the app"; the team's way in is the console (`shpyrd shell`, `shpyrd
  run`), and everyone sees the page. An allowlist would also need the real client address at
  the ingress controller, which the public door on OCI does not guarantee, and nginx's deny is
  a 403, not the page. No bypass for signed-in members either.
- Custom HTML per project. The page is the platform's state page with the project's name and
  the message; a page of a project's own can come later, as a ConfigMap the server serves.

## Proposal

- `spec.maintenance: {enabled, message, since, by}` on the App. The API writes `since` and
  `by` when the mode goes on; a new message leaves them; off clears the field.
- `PUT /api/projects/:slug/maintenance {enabled, message}` behind `project.deploy`, audited as
  `maintenance` with `on: <message>` or `off`. The message is plain text, at most 500 characters.
- `shpyrd maintenance [on|off] [--message]`; without a subcommand, says whether it is on, since
  when and by whom. `shpyrd projects list` says "Maintenance" for a running project behind the
  page; `projects info` has a line for it.
- Dashboard: a **Maintenance** card on the project's Overview (the switch and the message), the
  phase badge "Maintenance" in the header and on the launcher card (a wrench), and an alert
  under the header with **Serve the app again**.

## Design Details

- **The fence's HTTP half, reused.** RFC-0081's project operation fence (the annotation
  `shpyrd.io/project-maintenance`, set by a move, a restore or an import) already answers 503
  at the project's hosts: the controller rewrites both of the project's Ingresses, the app's
  and the edge companion's, to `/_shpyrd/maintenance` on the `shpyrd-edge` ExternalName Service,
  which points at shpyrd-server, and removes the sign-in annotations so no login flow starts.
  The mode drives the same rewrite. No image, no pod, no restart: switching is an Ingress
  change, live within a reload of nginx.
- **Two predicates in the controller.** `maintenanceHTTP` (the fence or the mode) drives the
  Ingresses, the edge Service of a public app and the sleep policy. `maintenance` (the fence
  alone) drives the drain of the processes, the 423 refusal of project writes, the shell
  refusal and the Postgres sleep guard. So the mode keeps the processes running and the API
  open, where the fence stops everything. If an operation starts while the mode is on, the
  fence takes over for its duration and the mode remains afterwards; while an operation runs
  the mode cannot be changed, as nothing can.
- **The page.** The server's `/_shpyrd/maintenance` answers by `Accept`. A browser gets
  `pages.ServiceUnavailable` (the shipyard at work) with the project's display name, looked up
  by the Host header the rewrite keeps, and the message, HTML-escaped; the page opens the app
  again by itself once a minute, and the page's script hash goes in the Content-Security-Policy.
  A program gets the JSON 503 with `Retry-After`, `Cache-Control: no-store` and
  `X-Shpyrd-Maintenance: true`, which the fence's confirmation check relies on. `Retry-After` is
  300 in the mode and 60 under an operation; crawlers take any 503 as temporary.
- **Sleep.** An app that sleeps is kept awake while HTTP is in maintenance, since no request
  would wake it and the work behind the page wants it up; the policy applies again afterwards.
  The sleep status says so.
- **Phase.** The phase stays the rollout's (Running, Deploying, Failed): the mode is a setting,
  not a state of the project, and what keys on Running (the deploy's wait, the release audit)
  is untouched. The list, the header and the launcher say "Maintenance" from
  `spec.maintenance.enabled`.
- **Not a release.** Turning the mode on or off records no release and bumps nothing but the
  spec's generation, like access and exposure.

## Open questions

Resolved on 2026-10-27:

1. Custom HTML per project? No, later (Non-Goals).
2. IP allowlist bypass? No (Non-Goals).
3. Status code 503 with `Retry-After`? Yes: 300 in the mode, 60 under an operation.

## Implementation History

- 2026-09-22: RFC written.
- 2026-10-27: decided: no bypass of any kind; the fence's HTTP half is reused behind a spec
  field rather than a maintenance image and Deployment.
- 2026-10-28: implemented: `spec.maintenance`, the controller's predicate split, the server's
  page and endpoint, `shpyrd maintenance`, the workspace card, badge and alert, the launcher
  card's phase, docs and changelog.
- Left: the cloud monitor reports the fence as health `maintenance` and does not know the mode
  yet. The fence keeps its words, "project maintenance" (or just "maintenance"), in the
  console's placement and backups screens.
