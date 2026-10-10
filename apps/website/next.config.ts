import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";
import { redirects } from "./src/lib/redirects";

// Every page is rendered when the site is built; two routes run on the
// server: the contact forms' (app/api/contact), on request, and the GitHub
// link's stars (app/api/github), kept an hour. The site runs on shpyrd as Next's
// standalone server (Dockerfile); Next serves the redirects itself
// (src/lib/redirects.ts), there and in the development server alike.
const config: NextConfig = {
  // One folder with the server and only the files it needs, traced from the
  // repository's root so the workspace's packages (design/ui, content) come.
  output: "standalone",
  outputFileTracingRoot: fileURLToPath(new URL("../..", import.meta.url)),
  redirects: async () => redirects,
  // The pictures are converted once and shipped as they are, never resized
  // on request.
  images: { unoptimized: true },
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
  // The development server hands its scripts only to pages opened at its own
  // address. A cloudflared quick tunnel (https://<random>.trycloudflare.com) is
  // how a proposal is shown to someone else, so its addresses may load them too;
  // so may an ngrok tunnel (https://<name>.ngrok-free.app, or .dev). It has
  // no part in the build.
  allowedDevOrigins: ["*.trycloudflare.com", "*.ngrok-free.app", "*.ngrok-free.dev"],
};

export default config;
