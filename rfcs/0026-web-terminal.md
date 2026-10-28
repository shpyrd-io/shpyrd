# RFC-0026 Web terminal

**Status:** implemented (v0.9.8, shpyrd-io/shpyrd#6; the bridge carries the CLI's shell, `run` and the database shells since v0.9.9 and v0.9.61) — see Closing below

**Owner:** Marcelo Paez Sequeira (merged in shpyrd-io/shpyrd#6)

**Depends on:** RFC-0005 (implemented), RFC-0008 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-28

## Closing (2026-10-28)

The RFC is closed as implemented. The Shell tab, the ticket, the WebSocket bridge, the
instance listing, the limits and the audit entries shipped in v0.9.8 as this RFC describes.
The non-goal "serving the CLI" fell the other way, as the text anticipated: the same bridge
runs a command for `shpyrd shell` since v0.9.9, and `shpyrd run`, `pg psql` and `redis cli`
reach their instances through it since v0.9.61 (RFC-0052), a database shell holding a slot
of its own (`pkg/api/resources_shell.go`); tickets are bound to their workspace since
v0.9.69. The dashboard the Shell tab was written for has been replaced: the tab is the
workspace application's (`apps/workspace/src/screens/project/shell.tsx`) and the terminal
is a component of the design system (`design/ui/src/components/terminal.tsx`), so the
browser-only checks of the old dashboard named below are history.

The four items under Implementation status were re-checked against the code on this date
and still stand: the audit write still goes through the request context
(`pkg/api/audit.go`), so `shell.close` is best-effort on a client disconnect and never
written on a server shutdown; nothing tests the two-second write deadline against a client
that stopped reading; `TERM` is still unset. The server still runs at one replica, so the
in-memory shell registry's assumption holds. Each is an issue of its own: the close audit on
a disconnect and on a shutdown, one fix for both (#281); the write-deadline test (#282);
`TERM` (#283).

## Summary

A shell into a running instance from the dashboard: xterm.js in a Shell tab, the server
bridging a WebSocket to `pods/exec`, the same launcher/bash/sh fallback as `shpyrd shell`,
gated by the `project.exec` role and audited.

## Motivation

`shpyrd shell` needs the CLI and a kubeconfig; developers with dashboard accounts only
have no shell.

### Goals

- Pick an instance, get a terminal, resize works, sessions end cleanly on tab close.
- Every session audited with actor and instance.

### Non-Goals

- Shells into build or run instances; file transfer.
- Serving the CLI. RFC-0052 routes `shell` and `run` through the API and will generalise
  this endpoint once it has its own requirements to design against; until then the
  protocol below is shaped for the browser alone.

## Proposal

Three routes. The two HTTP routes sit on the authenticated `/api` group, behind the
session middleware and a role check. The WebSocket does **not**: it is registered on the
public group, so no `s.auth()` runs in front of it and the handler is its own
authorization boundary — it takes the identity from the one-time ticket, re-resolves that
identity's roles itself, and refuses without `project.exec`. That placement is
deliberate, and it is the most security-relevant fact in this design: a browser cannot
set headers on a WebSocket handshake, and `shpyrd cluster dashboard` signs in with a
token in localStorage and so has no session cookie either, so a route requiring either
one would refuse exactly the people the shell is for. Anything added to this handler is
therefore unauthenticated until the ticket is redeemed.

- `GET /api/projects/{slug}/instances` (`project.exec`) lists the instances the selector
  offers: `{name, process, pod, ready}`, named by `logs.InstanceNames` so they match the
  log viewer, and filtered to app processes so build and run instances are absent.
- `POST /api/projects/{slug}/shell/ticket?instance=web.1` (`project.exec`) mints a
  one-time code, held in memory for 30 seconds and bound to the actor, project and
  instance. Being a POST, the existing session middleware already requires the CSRF
  header. Minting is rate limited per actor, since it is the cheapest way to make a
  single-replica control plane do repeated work (two pod LISTs, up to four `pods/exec`
  calls and two audit Events per session), and the number of outstanding tickets is
  capped.
- `GET /api/projects/{slug}/shell?instance=web.1&ticket=...` upgrades to a WebSocket: the
  handshake's `Origin`, when it carries one, must name the host this server answered on,
  the ticket is redeemed once, the role it recorded is checked again, then the server
  opens the exec stream with a TTY and pipes bytes both ways.

Browsers cannot set headers on a WebSocket, which is why the ticket exists and why it
carries the actor rather than leaning on the session cookie; the `Origin` check is the
conventional defence against cross-site WebSocket hijacking and costs nothing, so both
apply. Authorization is checked once, when the socket opens: revoking a grant stops the
next shell from opening but does not kill a shell already running, which ends when the
process exits, the tab closes, or the idle timeout reaps it.

Frames: binary carries terminal bytes in both directions. Text carries JSON control —
`{"type":"resize","cols":N,"rows":N}` from the client, and `{"type":"open","instance":…,
"shell":…}`, `{"type":"exit","code":N}` and `{"type":"error","message":…}` from the
server.

UI: a Shell tab on the project page with an instance selector and xterm.js (fit addon),
"Connecting to web.1", and the exit status when the process ends. The tab appears only
with `project.exec` and is lazy-loaded, so xterm stays out of the main bundle and no
socket opens until it is selected.

Limits: one shell per user per project at a time; idle timeout 30 minutes; every write to
the socket takes a two-second deadline, so a client that stops reading is dropped rather
than holding the session's only writer and with it the reap that would free the user's
slot; resolving the shell is bounded by 15 seconds, since it happens before the idle timer
is armed; inbound frames are capped at 1 MiB; a closed socket cancels the session, so a
process that ignores stdin does not outlive the tab. The server logs and audits
`shell.open` (instance and chosen shell) and `shell.close` (exit code, or the reason it
ended: an exit status, an idle reap, or the client disconnecting).

## Design Details

- `pkg/kexec` grows `StreamIO`: the existing `Stream` without the local terminal, taking
  explicit `io.Reader`/`io.Writer` and a channel of terminal sizes. `Stream` delegates to
  it, so the CLI keeps raw mode and SIGWINCH while the server reuses the same
  WebSocket-with-SPDY-fallback executor.
- The shell is resolved before the terminal opens: the same four candidates in the same
  order, each tried as a cheap non-TTY `kexec.Run` exiting immediately, then one TTY exec
  runs the winner. `shpyrd shell` instead tries them as the interactive session itself and
  reads the error text, which over a live socket risks writing a failed attempt's output
  to the terminal — the race `kexec.CountingWriter` exists to detect. Probing separately
  costs up to four round trips (one for a buildpack image) and keeps every failure
  invisible to the user. The two implementations may drift; the fallback order is the
  contract, not the code.
- The WebSocket authenticates by ticket alone, not by the session cookie: `shpyrd cluster
  dashboard` signs in with a token in localStorage and has no cookie, so a cookie
  requirement would break the shell for exactly the operator most likely to open it. The
  ticket therefore carries the actor, and the roles are resolved again at redemption so a
  grant revoked inside those 30 seconds still takes effect.
- Run pods live in the app's namespace labelled `shpyrd.io/process=run`, so the instance
  listing must exclude that label rather than merely listing the namespace. The
  controller's `runProcess` constant is unexported, so the API package repeats the
  literal; if a third caller needs it, it moves to `api/v1alpha1` instead of being copied
  again.
- The one-shell-per-user registry is in memory, which is correct only because the server
  runs a single replica (`deploy/components/shpyrd/base/server.yaml`). Scaling it out
  requires the limit to move into the control-plane store.
- The idle timer resets on client input, not server output: a chatty process must not hold
  a session open for someone who has walked away.
- Bound by the same network policy and PSS as `shpyrd shell`.
- Same-origin `wss:` satisfies the dashboard's existing `connect-src 'self'`, so the CSP
  is unchanged.
- The exec stream is reached through an injected seam, the way `Options.Apps` and
  `lookupTXT` already are, so role checks, tickets, limits, framing and timeouts are
  tested without a cluster.

## Implementation status

Audited on 2026-09-26 against the code, at the end of the branch's own review, and
re-checked on 2026-10-28 (commit 9e8f5ee9); three of its four entries closed the same day
(shpyrd-io/shpyrd#281, #282, #283). What the text above promises but the platform does not
do yet, or has not been seen doing on a cluster, is listed here.

- **Unverified on a cluster:** that the two-second write deadline drops a client which
  stopped reading. A unit test now isolates it (the idle timeout at its real 30 minutes, a
  client that reads nothing): the session ends within seconds and the trail says
  `closed: the client disconnected (it stopped reading)`. It has not been watched through
  the ingress, which is where a real stalled socket lives; a port-forward absorbs the
  back-pressure. The check belongs in `contrib/` (shpyrd-io/shpyrd#282).

- **Not implemented:** `TERM` for `shpyrd run` attached to a one-off instance. Its command is
  fixed in the pod, so the exec's `env TERM=…` wrapper does not reach it; setting it in the
  pod spec would.

Closed on 2026-10-28:

- A shutdown is audited. The server tells every live session itself (`shellLifetime`),
  since `srv.Shutdown` does not see hijacked connections: each is sent
  `{"type":"error"}`, its exec cancelled and `shell.close` written as
  `closed: server shutting down` within the shutdown budget, and new sessions are refused
  with 503 while it stops.
- `shell.close` survives a disconnect: audit writes take a context detached from the
  request (`context.WithoutCancel` and five seconds), so the cancellation they record no
  longer cancels them.
- `TERM`: the client names its terminal on the ticket (`?term=`; the browser
  `xterm-256color`, the CLI its own), and the shell runs through `env TERM=…` when the image
  has `env(1)`, bare otherwise. `xterm` when the client names none or something that is not a
  terminfo name. The CLI's kubeconfig path of `shpyrd shell` does the same.

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-26: picked up. Settled while designing: an instances endpoint, since the selector
  had no source of instance names; the shell resolved by one probe instead of the CLI's
  retry loop; and an `Origin` check alongside the ticket.
- 2026-09-26: implemented and reviewed. Corrections the reviews forced, worth recording
  because each was wrong in the plan rather than in the code: the instance selector excluded
  run pods with a `!=` label selector, which also matches pods carrying no process label at
  all and so offered a shell into BuildKit build pods, which run with unconfined seccomp and
  AppArmor; instance names were computed after filtering by phase, so a rolling restart could
  show the same pod under two different names in the Logs and Shell tabs; the terminal-size
  channel was closed while its only sender could still send, which panics in a bare goroutine
  that gin's `Recovery` does not catch and would have taken the server down; the socket had no
  inbound read limit, so one frame could grow a buffer until a single-replica control plane
  died; and every socket write was unbounded, so a client that stopped reading held the only
  writer and with it the reap that frees the user's slot.
  Verified against the kind dev cluster over a port-forward, with a project whose image has
  no CNB launcher so the probe had to fall through: the listing, the ticket lifecycle
  (404, 400, 409, replay, foreign `Origin`), a real `bash` in a real pod, `stty size`
  reporting the size a resize frame asked for, invalid UTF-8 arriving unmangled, `exit 3`
  propagating, the slot releasing, and `shell.open`/`shell.close` reaching the trail. The
  browser-only checks — xterm rendering, the CSP console, Strict Mode's double mount — and
  the items under Implementation status above are outstanding.
- 2026-09-27: merged as shpyrd-io/shpyrd#6 and the status set accordingly. It reads
  "implemented, gaps" rather than plain "implemented" because the four entries under
  Implementation status are promises this RFC's own text makes and the platform does not keep
  yet, which is what the index's `gaps` marker points a reader at.
  Of the browser-only checks, xterm rendering is settled: the owner opened the tab against
  the dev cluster, got a live `bash` prompt, and the padding around the canvas was adjusted
  in response to reading it. The CSP console and Strict Mode's double mount were not
  exercised and remain unverified. Colour was raised and deliberately left alone, which is
  the `TERM` gap below.
- 2026-09-26 (v0.9.9): the bridge runs a command for the CLI; `shpyrd shell` speaks the API.
- 2026-09-30 (v0.9.61): `shpyrd run`, `pg psql` and `redis cli` over the bridge (RFC-0052);
  a database shell holds a slot of its own.
- 2026-10-02 (v0.9.69): shell tickets bound to their workspace.
- 2026-10-28: closed as implemented; the four gaps re-checked and kept as issues #281, #282
  and #283, see Closing.
- 2026-10-28: three of the gaps closed (shpyrd-io/shpyrd#281, #282, #283): shutdown and
  disconnect audited, the write deadline tested on its own, `TERM` set. Left: the deadline
  seen through the ingress, and `TERM` for attached runs.
