---
id: shpyrd-spec-005
type: spec
title: What the project list shows, by role
status: active
date: 2026-11-25
related: []
governs: [pkg/api/apps.go, apps/workspace/src/screens/launcher.tsx, apps/workspace/src/lib/perms.ts]
---

## Purpose

`GET /api/projects` is the list of projects of a workspace. The launcher,
the first screen of the workspace application, draws one card per entry;
the CLI and other clients read the same list. This spec says which projects
a person gets in that list and how much of each, by their role on it.

## Behavior

For each project of the request's workspace, the person's effective role on
it decides the entry:

| The person may | Roles | Entry |
| -------------- | ----- | ----- |
| view the project (`project.view`) | viewer, developer, admin; workspace owner or admin; anyone while roles are not enforced | the full summary |
| only open the app (`project.open`) | user, reader | a card |
| neither | no role | none |

The effective role is the strongest of the person's direct grant and the
grants of their teams. The access mode of the app does not add an entry: a
person with no role on a public app gets no entry for it.

A **card** carries what the launcher shows and where the app opens, and
nothing of how the project is built or run:

- `id`, `slug`, `displayName`, `description`, `featured`;
- `url`, `domain`, `exposure`, `access`;
- `phase`, `asleep` (a process is sleeping), and `maintenance` reduced to
  `{"enabled": true}`, without its message or author;
- `icon`, `iconColor`, `iconUrl`, `iconType`;
- `role`, the person's own role on the project. A full summary has no
  `role`; a card always has one.

A card never carries the source, the image digest, the release, the
namespace, the status message, the catalog entry or state, the processes,
or the teams that hold a role on the project. It is a type of its own in
the server, so a field added to the full summary does not reach it.

Entries of both kinds are sorted together, by display name and then slug.

The uploaded image of a project, `GET /api/projects/:slug/icon`, is served
to whoever may open the app, so that a card can show it. Changing or
removing the image still takes the role that manages the project.

On the launcher, a card has the settings button only when the person may
view that project: a person who is `user` on one project and `developer` on
another gets the button on the second only. The button is decided per
project, from `role` on the entry, not from the person's roles as a whole.

## Interfaces

- `GET /api/projects`: a JSON array whose entries are full summaries or
  cards, as above. A client that needs fields of the full summary must
  skip entries that carry `role`.
- `GET /api/projects/:slug/icon`: requires `project.open`.
- Every other route under `/api/projects/:slug` keeps the action it
  required before; a `user` or a `reader` is refused there.

## Limits and non-goals

- The role table is not changed by this: `user` and `reader` hold
  `project.open` and nothing else.
- `GET /api/launcher` is a separate, older list that the workspace
  application does not read. This spec does not describe it.
- A card names no team. The page a person without a role is shown when
  they open a sign-in-required app is a different surface.
