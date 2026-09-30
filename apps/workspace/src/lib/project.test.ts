import { describe, expect, it } from "vitest";
import type { ProjectSummary } from "@/api/types";
import { ago, hostOf, openUrl, phaseOf, toneOf } from "./project";

const summary = (over: Partial<ProjectSummary>): ProjectSummary => ({ slug: "a", displayName: "A", namespace: "p-a", phase: "Running", release: 1, createdAt: "", access: "public", ...over });

describe("how a project is read for its card", () => {
  it("is running, failed, or deploying by its phase, and asleep when a process sleeps", () => {
    expect(phaseOf(summary({ phase: "Running" }))).toBe("running");
    expect(phaseOf(summary({ phase: "Failed" }))).toBe("failed");
    expect(phaseOf(summary({ phase: "Building" }))).toBe("deploying");
    expect(phaseOf(summary({ phase: "Running", processes: { web: { desired: 1, ready: 0, sleep: { state: "asleep" } } } }))).toBe("sleeping");
  });

  it("opens a public app at its address, and one behind sign-in through the bounce", () => {
    expect(openUrl("https://shop.acme.app", "public")).toBe("https://shop.acme.app");
    expect(openUrl("https://shop.acme.app/", "authenticated")).toBe("https://shop.acme.app/.shpyrd/signin?rd=%2F");
    expect(openUrl(undefined, "public")).toBe("#");
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
