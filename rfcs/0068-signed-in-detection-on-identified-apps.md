# RFC-0068 Signed-in detection on identified apps

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (in progress: the edge, access modes), RFC-0034 (implemented:
per-app custom domains)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

An app with access `identified` is public, and signed-in people are identified to it. Today
it identifies them only after the browser has crossed to the app host through
`/.shpyrd/signin` once — which the launcher and the project's Open button do, and a typed
URL or a bookmark does not. A person signed in at `demo.shpyrd.app` who opens
`expenses.demo.shpyrd.app` by hand is anonymous to the app until something sends them
through that path.

This RFC adds a **signed-in hint**: a cookie without any credential, set by the workspace's
dashboard host for the workspace's whole domain at sign-in, that tells the edge "this
browser has a session on this workspace". Seeing the hint on a page navigation to an
identified app that has no app cookie yet, the edge runs the existing sign-in bounce
silently, and the app knows who is there from the first page. Browsers without the hint —
strangers, crawlers, API clients — are never redirected, so public stays public. The
dashboard session itself still never reaches an app.

## Motivation

The edge keeps each app's cookie on the app's own host (`__Host-shpyrd_edge`) and the
dashboard's session on the dashboard host, on purpose: apps are third-party code, and a
cookie shared across `*.acme.shpyrd.app` would deliver the dashboard's session — a bearer
credential for the API — to every app's backend on every request. The price of that
isolation is that an app cannot see the dashboard's session, and for an `authenticated` app
that costs nothing: the edge answers 401 and nginx redirects through the bounce, which
completes without a prompt when the person is signed in. For an `identified` app the edge
must answer 200 to an anonymous request — that is what public means — so it never gets the
moment to bounce, and it cannot tell "a stranger" from "a signed-in person who has not
crossed yet".

The result is visible in the examples: `example-python.demo.shpyrd.app` greets a signed-in
owner as a visitor until they use the launcher or the app's own "Sign in" link, and it
contradicts the model's promise that every app is born knowing who its users are.

### Goals

- A person signed in at the workspace's dashboard is identified by every `identified` app
  of the workspace from their first request, however they arrive.
- Nobody else is redirected: an anonymous first-time visitor, a crawler, a `curl`, an API
  client of the app see exactly what they see today, with no extra hop.
- The dashboard's session cookie still never reaches an app; nothing an app receives lets
  it act as the person anywhere but in that app.
- No prompt appears to someone who did not ask to sign in: a stale hint (the session ended
  elsewhere) resolves to an anonymous page, not a login page.
- No redirect loops, whatever a browser does with cookies.

### Non-Goals

- Sharing one session cookie across app hosts (rejected below).
- Identification on per-app custom domains (RFC-0034: `app.acme.com`) without an explicit
  sign-in; an optional probe is an open question.
- Changing `authenticated` or `public` behaviour.

## Proposal

### The hint cookie

At sign-in, together with the session cookie, the dashboard host sets:

```
Set-Cookie: shpyrd_hint=1; Domain=<workspace domain>; Path=/; Secure; HttpOnly; SameSite=Lax; Max-Age=<session absolute lifetime>
```

`<workspace domain>` is the domain the workspace's apps live one label under: the
workspace address for an explicit workspace (`acme.shpyrd.app`, so the hint reaches
`*.acme.shpyrd.app`), the platform's apps domain for the implicit workspace (`<domain>`,
whose dashboard is `shpyrd.<domain>`; a host may set a cookie for its parent domain when
the parent is not a public suffix). When the dashboard host is not under the apps domain
(a custom dashboard URL) the hint is not set and nothing changes.

The value is the constant `1`. It is not a credential, is not linked to the session, and
grants nothing: an app that receives it learns that the browser has a session at the
workspace, which is exactly what `identified` entitles it to learn. It is cleared by
`/api/auth/logout`, by `/.shpyrd/logout` (which ends the dashboard session since v0.9.11),
and by the silent bounce when it finds no session.

### The edge

For an app with mode `identified`, the edge (`/edge/auth`) answers **401** instead of an
anonymous 200 when all of these hold:

1. no app cookie and no platform bearer identifies the caller (today's anonymous case);
2. the request carries `shpyrd_hint`;
3. the request is a page navigation: method `GET` or `HEAD`, and `Sec-Fetch-Dest: document`
   (falling back to `Accept` containing `text/html` for browsers without Fetch Metadata);
4. the request does not carry the loop guard `__Host-shpyrd_anon` (below).

Everything else — subresources, `fetch`/XHR, API clients, browsers without the hint — is
answered 200 with empty identity headers, as today. The controller adds the redirect
annotation identified Ingresses lack today, with a `silent` marker:

```
nginx.ingress.kubernetes.io/auth-signin: https://$http_host/.shpyrd/signin?silent=1&rd=$escaped_request_uri
```

so nginx turns that 401 into the existing bounce. The 401 is cached by nginx for 5 s per
cookie header like every other decision; the hint is part of `$http_cookie`, so hinted and
unhinted browsers never share a cache entry.

### The silent bounce

`/.shpyrd/signin?silent=1&rd=…` on the app host forwards `silent` to the dashboard host's
`/.shpyrd/start`. There:

- with a session: mint the one-time code and return to the app's `/.shpyrd/callback`, which
  sets the app cookie and redirects to `rd` — today's path, three hops, no page shown;
- without a session (the hint was stale): clear `shpyrd_hint` for the workspace domain and
  redirect straight back to `https://<app host><rd>` with `?shpyrd_anon=1`; the app host
  sets the loop guard `__Host-shpyrd_anon=1` (`Max-Age` 600 s) and redirects to `rd`
  without the parameter. No login page: the person did not ask to sign in.

An explicit `/.shpyrd/signin` (no `silent`) — the launcher, Open, an app's own "Sign in"
link — keeps today's behaviour and shows the login page when there is no session.

The loop guard exists for browsers that keep a cookie the dashboard host tried to clear
(a mismatched `Domain` attribute, an extension): the edge does not 401 while the guard is
present, so the worst case is one silent round trip every ten minutes for a browser with a
stale hint it will not drop.

### Metrics and audit

`shpyrd_edge_admissions_total` gains `reason="hint"` for admissions that followed a silent
bounce; a stale hint counts as `reason="hint_stale"` on the denials side (it is not a
denial of a person, so it is not audited).

### Alternatives considered

- **One session cookie for `*.<workspace>`** (the first text of RFC-0033): every app's
  backend receives the dashboard's session — a credential for the API — and a bug or a
  hostile app takes over the person's dashboard. Rejected; the per-host cookie stays.
- **Silent check in an iframe** (OpenID Connect session management): depends on cookies in
  a cross-site frame, which browsers block or will block, and on `acme.shpyrd.app` and
  `expenses.acme.shpyrd.app` being the same site — which the Public Suffix List entry for
  `shpyrd.app` (RFC-0033) makes false. Rejected.
- **Always probe on the first visit**: every anonymous visitor, crawler and API client of
  a public-looking app gets a redirect chain through the workspace host on first contact.
  Latency, SEO and a privacy tell for strangers who were promised a public page. Rejected
  as the default; kept as the optional probe for custom domains (open question 1).

## Design Details

- **Server.** `setSessionCookies` sets the hint with the domain from `tenant(c)` (address)
  or `Public.Domain`; `clearSessionCookies` and the edge logout clear it. `edgeAuth`: the
  identified branch checks hint, navigation and guard before answering 200. `edgeSignin`
  and `edgeStart` carry `silent`; `edgeStart` without a session and with `silent` clears
  the hint and redirects to the app with `shpyrd_anon=1`; the app host's `/.shpyrd/anon`
  (or the callback handling that parameter) sets the guard. Helpers: `hasHint(c)`,
  `isNavigation(c)` (Fetch Metadata first, `Accept` second).
- **Controller.** `edgeAnnotations` sets `auth-signin` for `identified` with `silent=1`;
  the annotation set is otherwise unchanged (a rollout of identified apps' Ingresses
  once; no pod restart).
- **Cache.** nginx's `auth-cache-key` already includes `$http_cookie`, so the hint and the
  guard are part of the key; the 401 lives 5 s, the 200 20 s.
- **Fetch Metadata.** `Sec-Fetch-Dest` is sent by every current browser on HTTPS
  navigations and forwarded to the auth subrequest with the other request headers; the
  `Accept` fallback keeps old browsers working and never affects API clients, which do not
  send `text/html`.
- **Domains.** Explicit workspace: hint on `Domain=<address>`. Implicit workspace: hint on
  `Domain=<apps domain>` set by `shpyrd.<apps domain>`. Local development
  (`127.0.0.1.nip.io`): `nip.io` is a public suffix, so `127.0.0.1.nip.io` is a registrable
  domain and accepts the cookie. Per-app custom domains: no hint; explicit sign-in as today.
- **Privacy.** The hint is `HttpOnly` and reaches only hosts under the workspace domain.
  On a self-hosted install whose apps domain is a company's whole domain, other hosts of
  that company receive a `shpyrd_hint=1` cookie; the operator may switch the hint off
  (`SHPYRD_EDGE_HINT=false`), which returns to today's behaviour.
- **Consent.** Identification without a click is the meaning of `identified`: the owner
  chose that the workspace's own apps know their people, as a company's intranet does.
  An app that must not know who visits is `public`.
- **Tests.** Edge tests: hint + navigation → 401 with the silent signin target; hint +
  `fetch` → 200 anonymous; no hint → 200 anonymous; guard present → 200; stale hint →
  hint cleared, guard set, no login page; explicit signin without session → login page.
  Controller test: the annotation on identified Ingresses.
- **Docs.** "Sign-in for your app": identified apps recognise signed-in people on the
  workspace's domain from the first page; on a custom domain, offer a sign-in link
  (`/.shpyrd/signin?rd=/`).

Estimated size: one day.

## Implementation status

Not started.

## Open questions

1. **Per-app custom domains.** Probe once per browser (a first navigation without hint or
   guard bounces through the workspace host and sets the guard either way), as an app-level
   setting `identify: probe`? Default: **no** for now — custom-domain apps keep the explicit
   sign-in link; revisit with workspace custom domains (RFC-0033 names, DNS and
   certificates), where the primary address changes anyway.
2. **Hint lifetime.** Session absolute lifetime (7 days) so a stale hint costs at most one
   silent round trip per app after the session ends; or the idle lifetime (12 h) to make
   stale hints rarer? Default: **absolute**.
3. **Guard lifetime.** 10 minutes balances "no loop" against "signed in a moment later,
   still anonymous in this app for a while". Default: **600 s**; an explicit sign-in
   clears it.

## Implementation History

- 2026-09-27: RFC written after the question "why does a signed-in person have to open
  `/.shpyrd/signin?rd=/` before an identified app sees them".
