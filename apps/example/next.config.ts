import type { NextConfig } from "next";

// Everything is compiled to static files (out/): no Node at run time.
const config: NextConfig = {
  output: "export",
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
};

export default config;
