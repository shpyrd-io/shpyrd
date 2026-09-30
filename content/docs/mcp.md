---
title: AI assistants (MCP)
description: Every workspace is a remote MCP server. Add it to Claude, sign in through the workspace, and ask about your projects.
---

Every workspace is a [Model Context Protocol](https://modelcontextprotocol.io) server. Add it to an assistant such as Claude as a connector, sign in through the workspace, and ask about your projects in plain language: *"check on Acme the metrics of the shop project for the last day"*. The assistant sees what you see and changes nothing. {% .lead %}

## Connecting Claude

1. On **Workspace › Overview › AI assistants**, copy the **Server URL** (`https://<workspace>/mcp`).
2. In Claude: **Settings › Connectors › Add custom connector**, paste the URL, **Connect**.
3. Claude sends you to your workspace's sign-in; sign in as usual and **Allow** the connection. The consent page names the assistant and what it may do.
4. In a conversation, enable the connector and ask. The server's name — what Claude shows — is set by a workspace admin on the same card; by default it is *"<workspace> on shpyrd"*.

Any MCP client that speaks Streamable HTTP with OAuth 2.1 works the same way: the workspace publishes its authorization server (`/.well-known/oauth-authorization-server`), accepts dynamic client registration and requires PKCE.

## What the assistant can do

| Tool | Answers |
| --- | --- |
| `list_projects` | the projects you can see, with status and URL |
| `get_project` | one project's status: phase, URL, processes and their instances, the current release, access mode, domains |
| `get_logs` | the most recent log lines (how many, which process) |
| `get_metrics` | requests, latency, errors, CPU and memory over a range, summarised (latest, average, peak) |

Everything is **read-only** and **within your roles**: a project you cannot open does not exist to the assistant; an owner's connection is a viewer's. Tools that change things (deploy, scale, config vars) come in a later release behind an explicit permission the consent page will name.

## Connections and revocation

Your connected assistants are listed on the same card with what they may do and when they were last used; **disconnect** revokes the assistant's access at once (a token it already holds stops within the hour). The assistant asks you to sign in again the next time. Admins see everyone's connections (`?all=true` on the API).

## For the platform operator

Nothing to configure: the OAuth server and the MCP endpoint answer at every workspace's address, with the workspace's dashboard as the token issuer and the platform's signing keys ([RFC-0032](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0032-mcp-connector.md)). Scripts that already hold a personal API token may call `/mcp` with it as a bearer instead of going through OAuth.
