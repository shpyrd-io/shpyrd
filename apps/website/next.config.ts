import type { NextConfig } from "next";

// Everything is compiled to static files (out/): no Node at run time.
const config: NextConfig = {
  output: "export",
  // A static export has no server to resize on request: the pictures are
  // converted once and shipped as they are.
  images: { unoptimized: true },
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
};

export default config;
