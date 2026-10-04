# RFC-0032 MCP connector

**Status:** in progress (first slice v0.9.16: the remote server with OAuth 2.1 and read tools)

**Owner:** Patrick Negri

**Depends on:** RFC-0031 (implemented), RFC-0033 (the workspace as the OAuth issuer)

**Creation date:** 2026-09-22

**Last update:** 2026-09-27


## Amendment (2026-10-04)

The MCP server and its OAuth 2.1 authorization server are an enterprise feature: they
moved to `ee/mcp` (source public, under the shpyrd Enterprise License) and serve with a
license; without one `/mcp` and the OAuth routes answer 402 and the dashboard hides the
Connections and MCP pages. The OAuth tables stay in the core's store. The server reaches
the API through `api.Host` (`pkg/api/host.go`), which a `RootExtension` receives.

## Summary

A Model Context Protocol server exposing shpyrd to AI agents (Claude Code, Cursor, Claude
Desktop): first as `shpyrd mcp` over stdio using the kubeconfig, then as a remote endpoint on
the server (streamable HTTP) authenticated with API tokens. Tools cover reading state and the
everyday operations; destroying is excluded.

## Motivation

Agents already run `shpyrd` commands by hand; a typed tool surface is safer and richer
(structured logs, metrics, releases).

### Goals

- `claude mcp add shpyrd -- shpyrd mcp` works; tools return structured JSON.
- Remote mode for hosted agents with per-user tokens and roles enforced.

### Non-Goals

- Autonomous remediation; anything without an explicit tool call.

## Proposal

- Tools: `list_projects`, `project_status`, `releases`, `logs` (window, process, filter),
  `metrics_summary`, `deploy_git`, `rollback`, `scale`, `set_config_vars` (names+values in,
  never out), `run_command` (one-off, returns output), `resources`, `audit`.
  Excluded: destroy project, delete resources, member changes.
- Resources (MCP): project overview documents; prompts: "diagnose failing instances".
- Local: `shpyrd mcp` speaks stdio, uses the kubeconfig like the CLI. Remote: `/mcp` on the
  server (streamable HTTP), `Authorization: Bearer shp_...`, roles enforced by the same
  middleware; every call audited with `via: mcp`.

## Design Details

- Go MCP SDK; tool schemas generated from Go structs; tests with a scripted client.

## Implementation status

**Shipped in v0.9.16 — the remote server, read-only, remote first.** The order of the
proposal was reversed: the hosted workspace is the product, so the remote endpoint came
first and the stdio `shpyrd mcp` is still to do.

- **Every workspace is an MCP server** at `https://<workspace>/mcp` (Streamable HTTP,
  stateless JSON-RPC: `initialize`, `ping`, `tools/list`, `tools/call`; no server-initiated
  streams, `GET /mcp` is 405). The name assistants show is a workspace setting (Overview ›
  AI assistants), "<workspace> on shpyrd" by default.
- **OAuth 2.1 instead of pasted tokens.** The workspace is an authorization server:
  discovery (RFC 8414 and RFC 9728, with and without the `/mcp` path), dynamic client
  registration (RFC 7591: https redirect URIs, or http on the loopback, or custom schemes),
  the authorization code flow with PKCE S256 required, a server-rendered consent page at the
  workspace host (the dashboard session signs the person in), access tokens that are the
  platform's EdDSA JWTs (`typ shpyrd-oauth`, one hour), refresh tokens that rotate (thirty
  days, revocable by the person on the Workspace page or by the client at `/oauth/revoke`).
  An access token is accepted by the whole API as the person, so tools call the API
  in-process and see exactly what the person sees. Personal `shp_` tokens work on `/mcp`
  too, for clients that can set a header.
- **Scopes**: `projects:read` (the default, and all the tools need) narrows the person's
  roles to viewing — a developer's token is a viewer's, an owner's a platform viewer's —
  and never holds the owner's actions; `projects:write` exists for the next slice.
- **Tools**: `list_projects`, `get_project` (status, processes, release, access, domains),
  `get_logs` (lines, process), `get_metrics` (range; latest, average and peak per series).
  Projects are named by slug or display name; a project the person may not see does not
  exist to the tool.
- Audit: `oauth.client.register`, `oauth.allowed`, `oauth.denied`, `oauth.token.issued`,
  `oauth.token.revoked`.

**Still to do**: the writing tools (`deploy_git`, `rollback`, `scale`, `set_config_vars`,
`run_command`) behind `projects:write` and a consent that says so; `releases`,
`resources`, `audit`; MCP resources and prompts (the "how do I read the user" snippet of
RFC-0033); the stdio `shpyrd mcp` for local agents; `via: mcp` on audit entries of tool
calls (they are audited as the person, via api); a test with Cursor (Claude's connector
was verified against the first cloud on 2026-09-27); the app-to-app promise of RFC-0033
(talking to other apps) is out of scope.

## Open questions

1. Tool set as above, never destroy? Default: yes.
2. Which clients to test with? Default: Claude (web and desktop connectors) for the remote
   server; Claude Code and Cursor when stdio lands.

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-27: first slice implemented (v0.9.16): the remote server at every workspace,
  OAuth 2.1 with PKCE and dynamic registration, four read tools; Claude connected to the
  first cloud and answered.
