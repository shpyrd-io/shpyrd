import { describe, expect, it } from "vitest";
import { discord } from "@shpyrd/content/site/offer";
import vercel from "../../vercel.json";

describe("the redirects", () => {
  it("send /discord to the invite the site's content names", () => {
    const to = vercel.redirects.find((r) => r.source === discord.href)?.destination;
    expect(to).toBe(discord.invite);
  });

  it("send /docs to the first page of the documentation", () => {
    expect(vercel.redirects.find((r) => r.source === "/docs")?.destination).toBe("/docs/getting-started");
  });
});
