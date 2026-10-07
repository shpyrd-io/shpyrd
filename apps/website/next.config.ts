import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";
import type { Redirect } from "next/dist/lib/load-custom-routes";
import vercel from "./vercel.json";

// Everything is compiled to static files (out/): no Node at run time. A static
// export has no redirects of its own, so they live in vercel.json, which
// Vercel answers before any file; the development server reads the same list.
const base: NextConfig = {
  output: "export",
  // A static export has no server to resize on request: the pictures are
  // converted once and shipped as they are.
  images: { unoptimized: true },
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
  // The development server hands its scripts only to pages opened at its own
  // address. A cloudflared quick tunnel (https://<random>.trycloudflare.com) is
  // how a proposal is shown to someone else, so its addresses may load them too;
  // so may an ngrok tunnel (https://<name>.ngrok-free.dev). It has no part in
  // the build.
  allowedDevOrigins: ["*.trycloudflare.com", "*.ngrok-free.dev"],
};

export default function config(phase: string): NextConfig {
  if (phase === PHASE_DEVELOPMENT_SERVER) {
    // JSON types "header" as any string; the file holds Vercel's redirects,
    // which are Next's too (has, missing).
    return { ...base, output: undefined, redirects: async () => vercel.redirects as Redirect[] };
  }
  return base;
}
