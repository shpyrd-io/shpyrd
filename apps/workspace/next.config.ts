import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

// In development the API is a shpyrd server somewhere else: the door of a
// cluster, given by SHPYRD_DEV_API, or a `go run ./cmd/shpyrd-server` on
// localhost:8080. The server sees the target's host, so the door (console
// or workspace) follows the address given. A static export allows no
// rewrites, so they exist here only.
//
//   SHPYRD_DEV_API=https://shpyrd.test npm run dev
const api = process.env.SHPYRD_DEV_API ?? "https://shpyrd.test";

export default function config(phase: string): NextConfig {
  if (phase === PHASE_DEVELOPMENT_SERVER) {
    // A local cluster presents certificates from the CA of its Caddy: the
    // `dev` script hands it to Node as it starts (NODE_EXTRA_CA_CERTS).
    return {
      agentRules: false,
      // The local Caddy may serve this development server at the door's
      // own name, so that the browser is on the door's origin: cookies and
      // the shell's socket are then its own.
      allowedDevOrigins: [new URL(api).hostname],
      rewrites: async () => [
        { source: "/api/:path*", destination: `${api}/api/:path*` },
        // Server-rendered pages: password reset and invitation activation.
        { source: "/account/:path*", destination: `${api}/account/:path*` },
      ],
    };
  }
  // Everything is compiled to static files (out/): no Node at run time.
  return { output: "export", agentRules: false };
}
