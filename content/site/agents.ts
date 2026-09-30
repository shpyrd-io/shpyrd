// Adding a workspace to a coding agent.
//
// Every workspace is a remote MCP server at `https://<workspace>/mcp`, speaking
// Streamable HTTP with OAuth 2.1 — that part is ours and is documented in
// /docs/mcp. The per-client snippets below are the CLIENTS' own configuration
// formats, not ours.
//
// VERIFIED: the Claude connector flow, which /docs/mcp describes step by step.
// NOT VERIFIED against each client's current release: the Claude Code, Cursor,
// VS Code and Codex snippets. They use each client's documented remote-MCP
// configuration, and config-file forms are preferred over CLI flags because
// they change less often. Check them before leaning on them in a campaign.
//
// There is no stdio path. RFC-0032 mentions `claude mcp add shpyrd -- shpyrd mcp`,
// but `shpyrd mcp` is not a command the CLI has — the RFC describes it as
// planned ("when stdio lands"). Do not advertise it.
//
// AHEAD OF THE PRODUCT, DELIBERATELY. The homepage says an agent deploys your
// apps. The four tools shipping today — list_projects, get_project, get_logs,
// get_metrics — are all annotated `readOnlyHint: true` in pkg/api/mcp.go, and
// /docs/mcp says plainly that deploy, scale and config vars "come in a later
// release behind an explicit permission". The maintainer chose this wording on
// 2026-09-29 knowing that. When the write tools land, this note comes out; until
// then the claim is a bet, and /docs/mcp is the page that contradicts it.

export const serverUrl = 'https://<workspace>/mcp'

export const agents = [
  {
    id: 'claude-code',
    name: 'Claude Code',
    kind: 'command',
    snippet: 'claude mcp add --transport http shpyrd https://<workspace>/mcp',
  },
  {
    id: 'claude',
    name: 'Claude',
    kind: 'steps',
    snippet: 'Settings › Connectors › Add custom connector\nhttps://<workspace>/mcp',
  },
  {
    id: 'codex',
    name: 'Codex',
    kind: 'file',
    file: '~/.codex/config.toml',
    snippet: '[mcp_servers.shpyrd]\nurl = "https://<workspace>/mcp"',
  },
  {
    id: 'cursor',
    name: 'Cursor',
    kind: 'file',
    file: '~/.cursor/mcp.json',
    snippet:
      '{\n  "mcpServers": {\n    "shpyrd": { "url": "https://<workspace>/mcp" }\n  }\n}',
  },
  {
    id: 'vscode',
    name: 'VS Code',
    kind: 'file',
    file: '.vscode/mcp.json',
    snippet:
      '{\n  "servers": {\n    "shpyrd": { "type": "http", "url": "https://<workspace>/mcp" }\n  }\n}',
  },
]

export const addToAgent = {
  label: 'Add to',
  note: 'Replace <workspace> with your workspace address. Your agent signs in as you, and works within the roles you already have.',
  docs: { label: 'Connector documentation', href: '/docs/mcp' },
}
