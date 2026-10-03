import { describe, expect, it } from "vitest";
import type { ProjectSummary } from "@/api/types";
import { addressOf, ago, badgesOf, hostOf, openUrl, phaseOf, toneOf } from "./project";

const summary = (over: Partial<ProjectSummary>): ProjectSummary => ({ slug: "a", displayName: "A", namespace: "p-a", phase: "Running", release: 1, createdAt: "", access: "public", ...over });

describe("how a project is read for its card", () => {
  it("is running, failed, or deploying by its phase", () => {
    expect(phaseOf(summary({ phase: "Running" }))).toBe("running");
    expect(phaseOf(summary({ phase: "Failed" }))).toBe("failed");
    expect(phaseOf(summary({ phase: "Building" }))).toBe("deploying");
  });

  it("is sleeping when the API reports a sleeping web process even if the project is running", () => {
    expect(phaseOf(summary({ phase: "Running", processes: { web: { desired: 0, ready: 0, sleep: { state: "sleeping" } } } }))).toBe("sleeping");
  });

  it.each(["awake", "waking", "unavailable"] as const)("keeps the project phase when its web process is %s", (state) => {
    expect(phaseOf(summary({ phase: "Running", processes: { web: { desired: 1, ready: 0, sleep: { state } } } }))).toBe("running");
  });

  it("opens a public app at its address, and one behind sign-in through the bounce", () => {
    expect(openUrl("https://shop.acme.app", "public")).toBe("https://shop.acme.app");
    expect(openUrl("https://shop.acme.app/", "authenticated")).toBe("https://shop.acme.app/.shpyrd/signin?rd=%2F");
    expect(openUrl(undefined, "public")).toBe("#");
  });

  it("answers at its own domain when one answers, else at its address under the platform", () => {
    expect(addressOf({ url: "https://shop.acme.shpyrd.app", domain: "shop.acme.com" })).toBe("https://shop.acme.com");
    expect(addressOf({ url: "https://shop.acme.shpyrd.app" })).toBe("https://shop.acme.shpyrd.app");
    expect(addressOf({})).toBeUndefined();
    expect(openUrl(addressOf({ domain: "shop.acme.com" }), "authenticated")).toBe("https://shop.acme.com/.shpyrd/signin?rd=%2F");
  });

  it("says who may open it: anyone, everyone, or its teams by name", () => {
    const url = "https://shop.acme.app";
    expect(badgesOf({ url, access: "public", teams: ["finance"] })).toEqual(["Anyone"]);
    expect(badgesOf({ url, access: "identified" })).toEqual(["Anyone"]);
    expect(badgesOf({ url, access: "authenticated", teams: ["everyone", "finance"] })).toEqual(["Everyone"]);
    expect(badgesOf({ url, access: "authenticated", teams: ["finance", "customer-care"] })).toEqual(["Finance", "Customer-care"]);
    expect(badgesOf({ url, access: "authenticated" })).toBeUndefined();
    expect(badgesOf({ access: "public", teams: ["platform"] })).toEqual(["Platform"]);
  });

  it("writes the address without the scheme, and the same ink for the same slug", () => {
    expect(hostOf("https://shop.acme.app")).toBe("shop.acme.app");
    expect(hostOf(undefined)).toBeUndefined();
    expect(toneOf("shop")).toBe(toneOf("shop"));
  });

  it("says how long ago in the unit that fits", () => {
    expect(ago(new Date(Date.now() - 5_000).toISOString())).toMatch(/^\ds ago$/);
    expect(ago(new Date(Date.now() - 3 * 60_000).toISOString())).toBe("3m ago");
    expect(ago(new Date(Date.now() - 5 * 3600_000).toISOString())).toBe("5h ago");
    expect(ago(new Date(Date.now() - 3 * 86400_000).toISOString())).toBe("3d ago");
  });
});
