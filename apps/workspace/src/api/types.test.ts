import { expect, it } from "vitest";
import { buildFailed } from "./types";

it("a branch shpyrd cannot read is no failed build", () => {
  const built = (reason: string) => ({ phase: "Failed", conditions: [{ type: "Built", status: "False", reason }] });
  expect(buildFailed(built("BuildFailed"))).toBe(true);
  expect(buildFailed(built("SourceUnreadable"))).toBe(false);
});
