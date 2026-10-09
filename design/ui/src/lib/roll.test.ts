import { describe, expect, it } from "vitest";
import { placeIn } from "./roll";

describe("placeIn", () => {
  it("shows the clock's item, lets the one before it leave, and keeps the rest waiting", () => {
    expect([0, 1, 2, 3].map((i) => placeIn(i, 2, 1))).toEqual(["waiting", "leaving", "here", "waiting"]);
  });

  it("has nothing leaving before the first turn", () => {
    expect([0, 1, 2].map((i) => placeIn(i, 0, -1))).toEqual(["here", "waiting", "waiting"]);
  });
});
