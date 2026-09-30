import type { NextConfig } from "next";

// The gallery: the library shown by itself. It is a tool for whoever
// designs; nothing of it is embedded in the server.
const config: NextConfig = {
  // Our own guides say what an agent needs (CLAUDE.md at the root); Next
  // would write an AGENTS.md and a CLAUDE.md here on every start.
  agentRules: false,
};

export default config;
