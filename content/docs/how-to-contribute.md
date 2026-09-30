---
title: How to contribute
description: Set up a development environment, find something to work on and send a change.
---

Shpyrd is developed in the open at [github.com/shpyrd-io/shpyrd](https://github.com/shpyrd-io/shpyrd) under the MPL-2.0 license. Issues, ideas and pull requests are welcome. {% .lead %}

## Where to start

- **Try it and report.** Run the [quick start](/) on your machine and open an issue for anything confusing, slow or broken. UX feedback is as valuable as code at this stage.
- **Pick an issue.** The [issue tracker](https://github.com/shpyrd-io/shpyrd/issues) has bugs and roadmap items. Comment on one before starting so work is not duplicated.
- **Propose a change.** Small fixes go straight to a pull request. Anything that changes behaviour or architecture starts as a short RFC (`rfcs/0000-template.md`) or an issue describing the problem first.
- **Talk.** Questions and discussions happen on [Discord](https://discord.gg/AxWMXXW7); the [code of conduct](https://github.com/shpyrd-io/shpyrd/blob/main/CODE_OF_CONDUCT.md) applies everywhere.

## Development environment

Docker, Go 1.27 and Node.js 22 (for the dashboard).

```shell
git clone https://github.com/shpyrd-io/shpyrd && cd shpyrd
make cli                      # ./bin/shpyrd
make dev-cluster              # kind cluster with everything except the shpyrd server
make dev-deploy               # build the server image, load it into kind, apply the shpyrd component
```

`make dev-deploy` again after changes to the server, controller or UI. For the dashboard alone, run `go run ./cmd/shpyrd-server --context kind-shpyrd --no-controller` in one terminal and `cd ui && npm run dev` in another (Vite proxies `/api`).

Useful targets:

```shell
make test vet                 # Go tests and vet
make generate                 # regenerate the App CRD and deepcopy code after editing api/
cd ui && npm run lint && npm run build
shpyrd cluster init --only shpyrd --set SHPYRD_SERVER_IMAGE=shpyrd-server:dev   # re-apply one component
```

Manifests under `deploy/` are embedded in the binaries: rebuild the CLI after editing them.

## Conventions

- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, `chore:`) and carry a DCO sign-off (`git commit -s`).
- Go code is `gofmt`ed and `go vet` clean; the UI is `oxlint` clean.
- Tests live next to the code: `internal/controller` (reconciler with a fake client), `pkg/api` (handlers with fake clients and an httptest Prometheus), `pkg/install` (every component renders offline).
- User-facing names follow the [concepts](/docs/concepts): projects, processes, instances, builds, releases, config vars.

## Documentation

This site lives in [shpyrd-io/shpyrd-docs](https://github.com/shpyrd-io/shpyrd-docs) (Next.js + Markdoc). Pages are Markdown files under `src/pages/docs`; the navigation is in `src/components/Layout.jsx`. Run `npm install && npm run dev` to preview.

## Support the project

Shpyrd is free and open source. If it saves you time, you can support its development with a donation: [donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01](https://donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01). Thank you.
