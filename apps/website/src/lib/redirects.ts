// The site's redirects, which Next serves itself (next.config.ts): on
// shpyrd, and anywhere else the site runs.
import type { Redirect } from "next/dist/lib/load-custom-routes";
import { discord } from "@shpyrd/content/site/offer";

export const redirects: Redirect[] = [
  // The bare domain is www's, path and all.
  {
    source: "/:path*",
    has: [{ type: "host", value: "shpyrd.io" }],
    destination: "https://www.shpyrd.io/:path*",
    permanent: true,
  },
  { source: discord.href, destination: discord.invite, permanent: false },
  { source: "/how-sharing-works", destination: "/getting-started", permanent: true },
  { source: "/docs", destination: "/docs/getting-started", permanent: false },
  // The prices in reais, for a visitor in Brazil, unless a currency was
  // chosen. A country header says so where the platform sends one (Vercel's);
  // shpyrd's front door does not, so a browser that prefers Brazilian
  // Portuguese says it there.
  {
    source: "/pricing",
    has: [{ type: "header", key: "x-vercel-ip-country", value: "BR" }],
    missing: [{ type: "query", key: "currency" }],
    destination: "/pricing/br",
    permanent: false,
  },
  {
    source: "/pricing",
    has: [{ type: "header", key: "accept-language", value: "pt-BR.*" }],
    missing: [{ type: "query", key: "currency" }],
    destination: "/pricing/br",
    permanent: false,
  },
];
