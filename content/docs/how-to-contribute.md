---
title: How to contribute
description: Set up a development environment, find something to work on and send a change.
---

shpyrd is developed in the open at [github.com/shpyrd-io/shpyrd](https://github.com/shpyrd-io/shpyrd) under the MPL-2.0 license. Issues, ideas and pull requests are welcome. {% .lead %}

## Where to start

- **Try it and report.** Follow [Getting started](/docs/getting-started), or [install it](/docs/installation) on your machine, and open an issue for anything confusing, slow or broken. UX feedback is as valuable as code at this stage.
- **Pick an issue.** The [issue tracker](https://github.com/shpyrd-io/shpyrd/issues) has bugs and roadmap items. Comment on one before starting so work is not duplicated.
- **Propose a change.** Small fixes go straight to a pull request. Anything that changes behaviour or architecture starts as a short RFC (`rfcs/0000-template.md`) or an issue describing the problem first.
- **Talk.** Questions and discussions happen on [Discord](https://discord.gg/RYAT4wNKfw); the [code of conduct](https://github.com/shpyrd-io/shpyrd/blob/main/CODE_OF_CONDUCT.md) applies everywhere.

## Development environment

Docker, Go 1.27 and Node.js 22 (for the applications, the design library and the website). Run `npm install` once at the repository root: the applications, `design/ui` and `content/` are one npm workspace.

```shell
git clone https://github.com/shpyrd-io/shpyrd && cd shpyrd
make cli                      # ./bin/shpyrd
make dev-cluster              # kind cluster with everything except the shpyrd server
make dev-deploy               # build the server image, load it into kind, apply the shpyrd component
```

`make dev-deploy` again after changes to the server or the controller.

The dashboard is two applications the server embeds: `apps/workspace` (a workspace's dashboard) and `apps/console` (the platform's). Each runs on its own with hot reload - `npm run dev` against a shpyrd server (`SHPYRD_DEV_API` names it; see the application's `next.config.ts`), or `npm run design` against sample data, with no server at all:

```shell
npm --prefix apps/workspace run design   # http://localhost:4325
npm --prefix apps/console run design     # http://localhost:4326
```

How an application is made, and how a design session runs, are in [apps/AGENTS.md](https://github.com/shpyrd-io/shpyrd/blob/main/apps/AGENTS.md) and [design/DESIGN_FOR_AGENT.md](https://github.com/shpyrd-io/shpyrd/blob/main/design/DESIGN_FOR_AGENT.md).

Useful targets:

```shell
make test vet                 # Go tests and vet
make generate                 # regenerate the App CRD and deepcopy code after editing api/
make ui                       # build the applications the server embeds
npm --prefix apps/workspace run lint && npm --prefix apps/workspace run test
shpyrd cluster init --only shpyrd --set SHPYRD_SERVER_IMAGE=shpyrd-server:dev   # re-apply one component
```

Manifests under `deploy/` are embedded in the binaries: rebuild the CLI after editing them.

## Conventions

- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, `chore:`) and carry a DCO sign-off (`git commit -s`).
- Go code is `gofmt`ed and `go vet` clean; the applications, `design/ui` and the website are `oxlint` clean, type-checked and tested (`npm run lint`, `npm run typecheck`, `npm run test` in each).
- Tests live next to the code: `internal/controller` (reconciler with a fake client), `pkg/api` (handlers with fake clients and an httptest Prometheus), `pkg/install` (every component renders offline).
- User-facing names follow the [concepts](/docs/concepts): projects, processes, instances, builds, releases, config vars.

## Documentation

These pages live in this repository. Each is a Markdown file (with [Markdoc](https://markdoc.dev) tags) under `content/docs/`, and the menu beside them is `content/navigation.ts`; the site that draws them is `apps/website` (Next, made of `design/ui`). To preview:

```shell
make website-dev              # http://localhost:4324/docs/getting-started
```

A saved page shows at once. `npm --prefix content run test` checks that every page is in the menu and every entry of the menu has a page. Besides Markdown, a page can use a few tags: `callout`, `quick-links`, `chat` with `message` for a conversation with an agent, and `agent-setup` for the tabs that show how to add shpyrd to each agent.

## Support the project

shpyrd is free and open source. If it saves you time, you can support its development with a donation: [donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01](https://donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01). Thank you.
