import { describe, expect, it } from "vitest";
import { headersOf } from "./cost-drains";
import { money, monthRange, rowName } from "./costs";

describe("costs, in words", () => {
  it("says amounts by currency", () => {
    expect(money({ USD: 1.234 })).toBe("1.23 USD");
    expect(money({ USD: 1, BRL: 5 })).toBe("5.00 BRL + 1.00 USD");
    expect(money({})).toBe("-");
  });

  it("names a row by its project's slug, the platform when it has none", () => {
    expect(rowName({ key: "a", project: "47zz", slug: "shop", cost: {}, lines: 1 }, "project")).toBe("shop");
    expect(rowName({ key: "b", process: "kube-system", cost: {}, lines: 1 }, "process")).toBe("the platform · kube-system");
    expect(rowName({ key: "c", resource: "ocid1.instance.x", cost: {}, lines: 1 }, "resource")).toBe("ocid1.instance.x");
  });

  it("is a month from its first day to the next month's", () => {
    expect(monthRange("2026-10")).toEqual({ from: "2026-10-01", to: "2026-11-01" });
    expect(monthRange("2026-12")).toEqual({ from: "2026-12-01", to: "2027-01-01" });
  });
});

describe("a drain's headers, as typed", () => {
  it("are Name=value lines, the value as it is", () => {
    expect(headersOf("Authorization=Bearer a=b\n\nX-Team = finance")).toEqual({ headers: { Authorization: "Bearer a=b", "X-Team": " finance" } });
  });

  it("refuse a line without a name", () => {
    expect(headersOf("just a token").error).toContain("Name=value");
  });
});
