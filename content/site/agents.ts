// Adding shpyrd to a coding agent.
//
// What an agent is given is shpyrd's public MCP server, https://mcp.shpyrd.io/mcp
// (the shpyrd-io/shpyrd-mcp repository): Streamable HTTP, no sign-in. It teaches
// the agent how to build an app that runs on shpyrd and holds the
// documentation; the agent does the work itself with the shpyrd CLI, signed in
// as the person. Each workspace has an MCP server of its own as well, at
// https://<workspace>/mcp, for reading its projects - /docs/mcp describes it.
//
// The per-client snippets below are the CLIENTS' own configuration formats,
// not ours. NOT VERIFIED against each client's current release: they use each
// client's documented remote-MCP configuration, and config-file forms are
// preferred over CLI flags because they change less often.

export const serverUrl = 'https://mcp.shpyrd.io/mcp'

export const agents = [
  {
    id: 'claude-code',
    name: 'Claude Code',
    kind: 'command',
    snippet: 'claude mcp add --transport http shpyrd https://mcp.shpyrd.io/mcp',
  },
  {
    id: 'claude',
    name: 'Claude',
    kind: 'steps',
    snippet: 'Settings › Connectors › Add custom connector\nhttps://mcp.shpyrd.io/mcp',
  },
  {
    id: 'codex',
    name: 'Codex',
    kind: 'file',
    file: '~/.codex/config.toml',
    snippet: '[mcp_servers.shpyrd]\nurl = "https://mcp.shpyrd.io/mcp"',
  },
  {
    id: 'cursor',
    name: 'Cursor',
    kind: 'file',
    file: '~/.cursor/mcp.json',
    snippet:
      '{\n  "mcpServers": {\n    "shpyrd": { "url": "https://mcp.shpyrd.io/mcp" }\n  }\n}',
  },
  {
    id: 'vscode',
    name: 'VS Code',
    kind: 'file',
    file: '.vscode/mcp.json',
    snippet:
      '{\n  "servers": {\n    "shpyrd": { "type": "http", "url": "https://mcp.shpyrd.io/mcp" }\n  }\n}',
  },
]

// The button downloads the shpyrd installer for the reader's computer: it
// installs the CLI, adds shpyrd's MCP server to their agents and offers to sign them in. The links are the
// installer-latest release, which always holds the newest installer. Where
// there is no installer for the computer (a phone, a Linux on ARM), the
// button goes to the manual install instead.
const installers = 'https://github.com/shpyrd-io/shpyrd/releases/download/installer-latest'

export const addToAgent = {
  label: 'Add to',
  note: 'It needs no account: it tells your agent how to build apps that run on shpyrd, and where the documentation is.',
  installers: {
    mac: { name: 'macOS', href: `${installers}/Shpyrd-Installer.dmg` },
    windows: { name: 'Windows', href: `${installers}/Shpyrd-Installer.exe` },
    linux: { name: 'Linux', href: `${installers}/Shpyrd-Installer-linux-amd64.tar.gz` },
  },
  manual: { label: 'or install manually', href: '/docs/getting-started#manually' },
}
