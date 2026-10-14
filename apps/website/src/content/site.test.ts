import { describe, expect, it } from "vitest";
import { boundaries, boundariesForStep } from "./boundaries";
import { access, active, familiarTools } from "./messages";
import { agents } from "./agents";

describe("the copy of the site", () => {
  it("gives every message family a headline and an explanation", () => {
    for (const m of [active, access, familiarTools]) {
      expect(m.headline).toBeTruthy();
      expect(m.explanation).toBeTruthy();
    }
  });

  it("writes the name in lower case, everywhere it appears", () => {
    const text = JSON.stringify([active, access, familiarTools, boundaries, agents]);
    expect(text).not.toMatch(/Shpyrd/);
  });

  it("gives each boundary a claim and the limit that goes with it", () => {
    for (const b of boundaries) {
      expect(b.claim, b.id).toBeTruthy();
      expect(b.limit, b.id).toBeTruthy();
    }
  });

  it("finds the boundaries of a step of the walkthrough", () => {
    expect(boundariesForStep("access").map((b) => b.id)).toContain("sign-in");
    expect(boundariesForStep("nothing-is-this")).toEqual([]);
  });

  it("gives every agent a name and something to paste", () => {
    for (const a of agents) {
      expect(a.name, a.id).toBeTruthy();
      expect(a.snippet, a.id).toContain("/mcp");
    }
  });
});
