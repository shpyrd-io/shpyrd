import { describe, expect, it } from "vitest";
import { number, percent, poolName } from "./overview";
import { queryString } from "../api/cluster-overview";

describe("the operational overview", () => {
  it("distinguishes absent telemetry from measured zero", () => {
    expect(number(undefined)).toBe("Unavailable");
    expect(percent(undefined)).toBe("Unavailable");
    expect(number(0)).toBe("0");
    expect(percent(0)).toBe("0%");
  });
  it("does not imply unlabeled nodes belong to a labeled pool", () => {
    expect(poolName("")).toBe("Default");
    expect(poolName("", true)).toBe("Unassigned");
    expect(poolName("builds", true)).toBe("builds");
  });
  it("preserves empty pool selection and encodes cursors and node filters", () => {
    const q = new URLSearchParams(queryString({ pool: "", continue: "a+b/==", search: "a&b", offset: 50, zone: undefined }));
    expect(q.get("pool")).toBe("");
    expect(q.get("continue")).toBe("a+b/==");
    expect(q.get("search")).toBe("a&b");
    expect(q.has("zone")).toBe(false);
  });
});
