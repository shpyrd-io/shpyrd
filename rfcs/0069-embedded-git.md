# RFC-0069 Embedded git: the platform keeps the source

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0004 (implemented), RFC-0031 (implemented), RFC-0032 (in progress),
RFC-0033 (in progress), RFC-0046 (implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

Every project gets a git repository hosted by the platform, at
`https://<workspace>/git/<project>.git`. What runs is always a commit in it: `shpyrd deploy`
commits the uploaded tree, `git push` deploys, an assistant working through the MCP
connector reads and changes code there, and a company that leaves a partner leaves with its
code, because the code was never anywhere else. The repository is the project's source of
truth and its history; releases point at commits.

This revisits RFC-0050 (git push deploys, rejected) with a different motivation: not a
Heroku-style habit for developers who have GitHub, but a home for the source of apps built by
people who have no repository at all, and a surface agents can work on.

## Motivation

- **Small software has no repository.** A product manager who builds an app with an AI
  coding tool has a folder on a laptop. `shpyrd deploy` uploads it; the platform keeps the
  archive of the last deploys and nothing else. When the laptop goes, the app cannot be
  changed again. RFC-0033's Story 1 assumes the platform is where the app lives.
- **The partner story needs it.** RFC-0033's Story 2 promises that a customer who leaves a
  consulting shop is not hostage: apps *and their source* stay with the customer. Today the
  source is wherever the consultant kept it.
- **Agents change code, not archives.** RFC-0032's connector answers about projects; the
  next step is an assistant that fixes the bug it found. It needs a repository to clone,
  commit to and push, with the platform building the result — and the person seeing the
  diff.
- RFC-0018 (repository monitoring) and RFC-0054 (GitHub App) serve people who already have
  GitHub; they stay. This is for the rest, and for the platform's own view of the source.

### Goals

- A repository per project without setup: it exists when the project does.
- `shpyrd deploy` from a folder commits and pushes for the person (they never run git);
  `git push shpyrd main` works for those who do; both build the pushed commit.
- Releases record the commit; the project page shows the history and lets a person roll
  back to a commit (a new release built from it), not only to a previous image.
- Access follows project roles: `developer` pushes, `viewer` clones, `user` nothing.
- The repository is in the platform backup and in the workspace export.

### Non-Goals

- Pull requests, reviews, issues, a web editor: not a forge. Code review happens in the
  tools people already have; a repository can be mirrored to GitHub (push mirror) later.
- Replacing RFC-0018/0054: a project whose source is on GitHub keeps deploying from there;
  its embedded repository mirrors what was built, read-only.

## Proposal

- **Storage.** Bare repositories in the platform's object storage (RFC-0046) through a
  git object database backed by the bucket, or on a `storage-rwx` volume (RFC-0041) when
  that lands — one of the two (open question 1). Size cap per project (default 500 MB),
  counted in the plan (RFC-0042).
- **Server.** The smart HTTP protocol (`git-upload-pack`, `git-receive-pack`) served by the
  platform server at `/git/<project>.git`, authenticated with a personal API token
  (RFC-0031) or an OAuth access token (RFC-0032) as the HTTP password (the user name is
  ignored), authorised by the project role: `developer` and above may push, `viewer` may
  fetch. `shpyrd deploy` gets a `git` transport: the CLI commits the folder on the
  server's behalf (the server does the commit: the person's tree, their name and email
  from the identity, a message "Deploy from shpyrd CLI").
- **Build on push.** A push to the default branch makes a release: the receive hook writes
  the commit into `spec.source.git.revision` of the App and the existing build pipeline
  (kpack or BuildKit, RFC-0004) fetches from the embedded repository through the sources
  port, the same way it fetches uploaded archives today. The `git push` output streams the
  build log (RFC-0050's one good idea). Other branches build nothing (previews are
  RFC-0055's, deferred).
- **Releases and rollback.** `status.releases[].commit` records what was built; "roll back
  to this commit" builds it again as a new release (distinct from the image rollback of
  today, which stays for images that were not built from the repository).
- **Agents.** The MCP connector gains tools in a later slice (`read_file`, `propose_change`
  → a commit on a branch, `deploy_branch`), all against the embedded repository, all within
  `projects:write`.
- **Export.** `shpyrd projects export` (or the workspace export) includes the repository
  as a bundle; the platform backup (RFC-0037) includes every repository.

## Design Details

- Implementation: `go-git` for the object database and the transport (no `git` binary in
  the image); object storage backend through the bucket credential the project already has
  (RFC-0046 gives each consumer a key); receive-pack hook in-process.
- The repository is created on project creation (empty) and on the first deploy; a project
  created from `--git` (RFC-0017/0018) gets a read-only mirror updated at each build.
- Identity of commits: `Author: <name> <email>` from the person; `Committer: shpyrd`.
- Audit: `git.push` (who, branch, commit), `git.rollback`.
- CLI: `shpyrd git url` (prints the remote and how to add it), `shpyrd deploy --commit
  "message"`.

## Open questions

1. Object storage or a shared volume for the repositories? Default: **object storage**
   (exists on every cloud profile; the volume is RFC-0041, not built).
2. Does `shpyrd deploy` always commit, or only when the project opted in? Default: **always**
   once the RFC ships; the repository is part of what a project is.
3. Push mirror to GitHub/GitLab as part of this RFC? Default: **no**, a follow-up.

## Implementation History

- 2026-09-27: RFC written (research; replaces the reference "RFC-0064 embedded git" in
  RFC-0033, whose number went to the Linux developer loop).
