import { describe, expect, it } from "vitest";
import { bytes } from "./format";

describe("bytes", () => {
  it("writes a size with the unit that fits it", () => {
    expect(bytes(0)).toMatch(/0/);
    expect(bytes(1024 * 1024)).toMatch(/M/);
  });
});
