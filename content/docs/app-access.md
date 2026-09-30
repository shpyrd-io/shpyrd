---
title: Sign-in for your app
description: Who may open an app is decided at the edge, before a request reaches it - visitors sign in, teams get the user role, and the app receives who they are in headers and a signed JWT. No sign-in code in the app.
---

New projects ask visitors to sign in. Only people with a role on the project get in, and every request the app receives says who they are — a header to read, or a JWT to verify. The app itself has no sign-in code, no user table, no sessions to manage; roles are the teams and grants you already have. {% .lead %}

## Three access modes

| Mode | Who opens the app | What the app receives |
| --- | --- | --- |
| **Sign-in required** (`authenticated`, the default for new projects) | People with a role on the project: teams or people with the `user` role, and its viewers, developers and admins. Everyone else is asked to sign in, or told which team the app is available to. | Identity on every request |
| **Public** (`public`) | Anyone on the internet — a site, a landing page. Projects created before v0.5 are public. | Nothing |
| **Public, signed-in visitors identified** (`identified`) | Anyone; people who happen to be signed in are identified. A site with a "Hi, Maria" corner. | Identity when there is one |

In `identified` mode nobody is asked to sign in, so a visitor is identified only once their browser holds the app's cookie: the launcher and the project page's **Open** button send people through `https://<app host>/.shpyrd/signin?rd=/` (silent when they are signed in), and your app can link there itself — "Hello, anonymous visitor — [sign in](/.shpyrd/signin?rd=/)". The [examples](https://github.com/shpyrd-io/shpyrd-examples) do exactly that.

```shell
shpyrd projects create "Expenses"            # sign-in required
shpyrd projects create "Landing page" --public
shpyrd access --project expenses              # who may open it
shpyrd access set public --project expenses   # anyone
```

The dashboard has the same switch on the project page (Access card); making an app public asks for confirmation, and a badge says "Anyone can open this app" while it is.

## Who gets in

Grant the `user` role to a team or a person; `viewer`, `developer` and `admin` open the app too (they already read its logs and configuration):

```shell
shpyrd teams create finance --member joao@example.com --group Finance   # or a group of your identity provider
shpyrd members add expenses --team finance --role user
shpyrd members add expenses --user pedro@example.com --role user
```

The built-in **everyone** team is every person who has signed in: `shpyrd members add expenses --team everyone --role user` opens the app to the whole company at once.

Someone signed in without a role sees a page saying "Expenses is available to the finance team" (the teams that hold the `user` role, never people) — with a link to sign in as someone else — instead of the app. Someone who is not signed in is taken to the platform's sign-in page and back to the app afterwards; an API client (nothing in its `Accept` asks for HTML) gets a JSON `401` with `WWW-Authenticate` instead. People whose only roles are `user` see a **launcher** in the dashboard: tiles for the apps they can open, nothing else.

### Scripts, CI and agents

A personal API token (Workspace › API tokens, or `shpyrd tokens create`) opens a closed app the way its owner would, within the token's roles:

```shell
curl -H "Authorization: Bearer shp_…" https://expenses.acme.shpyrd.app/api/report
```

The app receives the owner's identity (`X-Shpyrd-User`, the JWT with `sub` = the person's id and `provider: api-token`) and the owner's teams. A token with no role on the project is refused with `403`; a revoked one with `401`. Any other bearer is treated as anonymous: an authenticated app asks it to sign in, an `identified` app receives it as sent — so a public app can run its own API authentication behind the door.

## What the app receives

Every request that reaches an app with sign-in required (or an identified visitor of an `identified` app) carries:

```
X-Shpyrd-User:   joao@example.com
X-Shpyrd-Email:  joao@example.com
X-Shpyrd-Name:   João Silva
X-Shpyrd-Teams:  finance,everyone
X-Shpyrd-Roles:  user
Authorization:   Bearer eyJhbGciOiJFZERTQSIs...
```

The headers are set by the platform's edge on every request — whatever a client sends in them is blanked at the front door, for public apps too (since v0.9.4), and replaced with the real values where sign-in is on — and your app is reachable only through that edge (its network policy admits the ingress and nothing else from outside the project). Reading `X-Shpyrd-User` is enough for most internal apps:

```python
# Flask
who = request.headers.get("X-Shpyrd-User", "anonymous")
teams = request.headers.get("X-Shpyrd-Teams", "").split(",")
if "finance" not in teams:
    abort(403)
```

```js
// Express
const who = req.get("X-Shpyrd-User") ?? "anonymous";
const teams = (req.get("X-Shpyrd-Teams") ?? "").split(",").filter(Boolean);
```

```go
// Go
who := r.Header.Get("X-Shpyrd-User")
teams := strings.Split(r.Header.Get("X-Shpyrd-Teams"), ",")
```

### Verifying the JWT

For apps that also accept calls from elsewhere, or that want proof rather than a header, the `Authorization` bearer is a JWT signed by the platform (EdDSA / Ed25519), five minutes long, with the key published at `https://shpyrd.<your domain>/.well-known/jwks.json`:

```json
{
  "iss": "https://shpyrd.example.com",
  "aud": "expenses",
  "sub": "idn_01J...",
  "email": "joao@example.com",
  "name": "João Silva",
  "ws": "default",
  "project": "expenses",
  "roles": ["user"],
  "teams": ["finance", "everyone"],
  "realm": "workspace",
  "provider": "google",
  "iat": 1790000000,
  "exp": 1790000300
}
```

Check `iss`, `aud` (your project's slug) and `exp` with any JWT library that supports EdDSA (`jose`, `PyJWT[crypto]`, `github.com/lestrrat-go/jwx`). `roles` holds the caller's role on this project and, for platform admins, their platform role; `teams` the teams they belong to.

The keys live at **`<iss>/.well-known/jwks.json`** — the issuer is the dashboard URL of the workspace the app belongs to, so the same code works wherever the app runs. Every process is told what to expect through the environment (since v0.9.4):

| Variable | Example | Use |
| --- | --- | --- |
| `SHPYRD_ISSUER` | `https://shpyrd.example.com` | the expected `iss`; fetch the JWKS at `$SHPYRD_ISSUER/.well-known/jwks.json` |
| `SHPYRD_PROJECT` | `expenses` | the expected `aud` |
| `SHPYRD_WORKSPACE` | `default` | the expected `ws` |

The [`examples/hello`](https://github.com/shpyrd-io/shpyrd/tree/main/examples/hello) app verifies the token with nothing but the Go standard library (`cmd/web/jwt.go`: fetch the JWKS, cache it, check the Ed25519 signature by `kid`, then `iss`, `aud` and `exp`), and its page says whether the visitor was verified or merely read from the headers.

## Open as: seeing the app the way a team does

Builders have no test users. On the project page, **Open as** opens the app in a new tab with a preview identity: your account, but the teams you chose (or none, or anonymous). The app sees a member of Finance; the token carries `"preview": true` and an `act` claim naming you, so an app can tell if it wants to. Previews are recorded in the project's audit trail.

```shell
shpyrd members list expenses       # what a team would get
```

## How it works

- The app's Ingress carries ingress-nginx's `auth_request` annotations: every request is checked with the platform's server first (a subrequest, cached for a few seconds).
- Signing in happens on the platform's host. The browser is sent there and comes back to the app's host with a one-time code, which becomes a cookie **for that app's host only** (`__Host-shpyrd_edge`). Your app never sees the dashboard's session cookie, and a cookie for one app opens nothing else.
- Signing out of the dashboard ends every app cookie: each request checks that the session still exists. Signing out of an app (`/.shpyrd/logout` on its host) ends the same session, so the dashboard and every other app close too.
- The keys apps verify the JWT against rotate every 30 days; a retired key stays in the JWKS for a week, so verify by `kid` and refetch the JWKS when you meet one you do not know (the example's verifier does).
- A companion Ingress for `/.shpyrd/` on the app's host serves the sign-in bounce, the callback, the sign-out and the "available to team X" page; the path is reserved for the platform.
- Operators reach apps with the admin token (`Authorization: Bearer <token>`); scripts and CI can too. Pasting the token on the sign-in page opens a browser session the same way accounts do.

## Between projects

Projects are network-isolated by default: nothing else running on the cluster may reach an app, even from the same workspace, except the front door, the platform and monitoring. A project opens itself to another with its allow list — on its page (**Connections**), with `shpyrd allow add project crm --project expenses`, or in `shpyrd.yaml`:

```yaml
allow:
  - project: crm       # crm may call expenses through expenses' Service or hostname
```

The listed project must be one of the workspace's own: allows never cross a workspace, and the platform refuses a name that is not there. Changes apply within seconds and create no release. The caller reaches the callee at `http://<callee>-web.<callee namespace>.svc` inside the cluster (or through its public hostname, which goes through the front door and the callee's access mode).

## Not yet

OAuth 2.1 for AI agents (personal tokens work today), and disabling previews per project.
