# The console application

The console: the operator's application, at the console host. The
cluster as it is, the workspaces it hosts, the accounts, the console's
own sign-in, the platform's knobs. Made of `design/ui`, the same way as
`apps/workspace`.

## Run

```bash
npm install                                  # at the repository root
npm --prefix apps/console run design         # the Mock, http://localhost:4326
npm --prefix apps/console run dev            # against a server, the console door
SHPYRD_DEV_API=https://shpyrd.shpyrd.test npm --prefix apps/console run dev
npm --prefix apps/console run build          # static files in out/
```

In development `/api` and `/account` go to `SHPYRD_DEV_API` (by default
`https://shpyrd.shpyrd.test`, the console door of a local kind cluster).
Built, the application is static files served by the server it calls.

`NEXT_PUBLIC_API_MODE=mock` answers from `mock/*.json`, kept in
localStorage; `npm run design` sets it.
