import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";
import type { Redirect } from "next/dist/lib/load-custom-routes";
import vercel from "./vercel.json";

// Every page is rendered when the site is built; one route runs on request,
// the contact forms' (app/api/contact). The site runs on shpyrd as Next's
// standalone server (Dockerfile); the redirects in vercel.json are Next's
// own, so they hold there, on Vercel and in the development server alike.
const config: NextConfig = {
  // One folder with the server and only the files it needs, traced from the
  // repository's root so the workspace's packages (design/ui, content) come.
  output: "standalone",
  outputFileTracingRoot: fileURLToPath(new URL("../..", import.meta.url)),
  // JSON types "header" as any string; the file holds Vercel's redirects,
  // which are Next's too (has, missing).
  redirects: async () => vercel.redirects as Redirect[],
  // The pictures are converted once and shipped as they are, never resized
  // on request.
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

export default config;
