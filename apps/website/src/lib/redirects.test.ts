import { describe, expect, it } from "vitest";
import { discord } from "@shpyrd/content/site/offer";
import config from "../../next.config";
import { redirects } from "./redirects";

const find = (source: string, has?: string) =>
  redirects.filter((r) => r.source === source && (has === undefined || (r.has ?? []).some((h) => ("key" in h ? h.key : h.type) === has)));

describe("the redirects", () => {
  it("send /discord to the invite the site's content names", () => {
    expect(find(discord.href)[0]?.destination).toBe(discord.invite);
  });

  it("send /docs to the first page of the documentation", () => {
    expect(find("/docs")[0]?.destination).toBe("/docs/getting-started");
  });

  // Next serves them itself, wherever it runs: on shpyrd, on Vercel while
  // it still serves the domain, and in the development server.
  it("are Next's own, wherever it runs", async () => {
    expect(await config.redirects?.()).toEqual(redirects);
  });

  it("send the bare domain to www, keeping the path", () => {
    const [apex] = find("/:path*", "host");
    expect(apex).toMatchObject({ has: [{ type: "host", value: "shpyrd.io" }], destination: "https://www.shpyrd.io/:path*", permanent: true });
  });

  it("send a browser that prefers Brazilian Portuguese to the prices in reais, unless a currency was chosen", () => {
    const [br] = find("/pricing", "accept-language");
    expect(br).toMatchObject({ destination: "/pricing/br", missing: [{ type: "query", key: "currency" }] });
  });
});
