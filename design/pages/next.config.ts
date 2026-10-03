import type { NextConfig } from "next";

// The pages the server serves by itself, drawn from the library and
// packed into one file each (scripts/pack.mjs) for pkg/pages to embed.
const config: NextConfig = {
  output: "export",
  agentRules: false,
  // The badge of the development server would be drawn in every frame.
  devIndicators: false,
};

export default config;
