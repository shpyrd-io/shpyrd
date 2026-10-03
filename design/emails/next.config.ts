import type { NextConfig } from "next";

// The emails the platform sends, drawn here and packed into one Go
// template each (scripts/pack.mjs) for pkg/emails to embed. The badge of
// the development server would be drawn in every email's frame.
const config: NextConfig = {
  output: "export",
  agentRules: false,
  devIndicators: false,
};

export default config;
