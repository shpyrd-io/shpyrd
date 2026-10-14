import createMDX from "@next/mdx";
import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";

// Everything is compiled to static files (out/): no Node at run time.
const config: NextConfig = {
  output: "export",
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
};

export default createMDX({ options: { remarkPlugins: ["remark-frontmatter", "remark-gfm", fileURLToPath(new URL("../shared/src/editorial/remark.mjs", import.meta.url))] } })(config);
