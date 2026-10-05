import { describe, expect, it } from "vitest";
import { hoursPerMonth, lineCost, prices, pricing, rates } from "./pricing";

describe("the pricing page", () => {
  it("costs each line of the example from the usage prices", () => {
    expect(lineCost({ cpuCores: 1, hours: hoursPerMonth })).toBeCloseTo(65.7);
    expect(lineCost({ memoryGib: 1, hours: hoursPerMonth })).toBeCloseTo(14.6);
    expect(lineCost({ storageGib: 5 })).toBeCloseTo(0.85);
    expect(lineCost({ egressGib: 10 })).toBeCloseTo(0.5);
    expect(lineCost({})).toBe(0);
  });

  it("comes to more than the minimum for the example, so the usage is what is paid", () => {
    const total = pricing.example.lines.reduce((sum, line) => sum + lineCost(line), 0);
    expect(total).toBeCloseTo(7.16, 2);
    expect(Math.max(total, pricing.example.minimum)).toBe(total);
  });

  it("shows each paid plan's rates as the prices it charges", () => {
    for (const id of ["starter", "pro"] as const) {
      const plan = pricing.plans.find((p) => p.id === id)!;
      const shown = (line: string) => plan.usage.find((u) => u.id === line)?.value;
      expect(shown("cpu"), id).toBe(`$${rates[id].cpuCoreHour}`);
      expect(shown("memory"), id).toBe(`$${rates[id].memoryGibHour}`);
      expect(shown("storage"), id).toBe(`$${rates[id].storageGibMonth}`);
      expect(shown("egress"), id).toBe(`$${rates[id].egressGib}`);
    }
  });

  it("costs the example on Starter", () => {
    expect(prices).toBe(rates.starter);
  });

  it("gives every plan the same usage lines, in the same order", () => {
    const order = pricing.usage.map((line) => line.id);
    for (const plan of pricing.plans) {
      expect(plan.usage.map((line) => line.id), plan.id).toEqual(order);
    }
  });

  it("keeps Enterprise out of the priced plans: a conversation, not a price", () => {
    expect(pricing.plans.map((p) => p.id)).toEqual(["free", "starter", "pro"]);
    expect(pricing.enterprise).not.toHaveProperty("price");
    expect(pricing.enterprise).not.toHaveProperty("usage");
    expect(pricing.enterprise.action).toEqual({ label: "Talk to us", kind: "contact" });
  });

  it("names one point of Enterprise for each licensed feature of ee/", () => {
    expect(pricing.enterprise.features).toHaveLength(4);
  });
});
