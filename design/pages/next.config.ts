import type { NextConfig } from "next";

// The pages the server serves by itself, drawn from the library and
// packed into one file each (scripts/pack.mjs) for pkg/pages to embed.
const config: NextConfig = {
  output: "export",
  agentRules: false,
};

export default config;
