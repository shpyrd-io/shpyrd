import { describe, expect, it } from "vitest";
import { hoursPerMonth, lineCost, prices, pricing } from "./pricing";

describe("the pricing page", () => {
  it("costs each line of the example from the usage prices", () => {
    expect(lineCost({ cpuCores: 1, hours: hoursPerMonth })).toBeCloseTo(14.6);
    expect(lineCost({ memoryGib: 1, hours: hoursPerMonth })).toBeCloseTo(3.65);
    expect(lineCost({ storageGib: 5 })).toBeCloseTo(0.5);
    expect(lineCost({ egressGib: 10 })).toBeCloseTo(0.5);
    expect(lineCost({})).toBe(0);
  });

  it("comes to less than the minimum for the example, so the minimum is what is paid", () => {
    const total = pricing.example.lines.reduce((sum, line) => sum + lineCost(line), 0);
    expect(total).toBeCloseTo(2.11, 2);
    expect(Math.max(total, pricing.example.minimum)).toBe(5);
  });

  it("shows each plan's rates as the prices it charges", () => {
    const starter = pricing.plans.find((plan) => plan.id === "starter")!;
    const rate = (id: string) => starter.usage.find((line) => line.id === id)?.value;
    expect(rate("compute")).toContain(`$${prices.cpuCoreHour}`);
    expect(rate("memory")).toContain(`$${prices.memoryGibHour}`);
    expect(rate("storage")).toContain(`$${prices.storageGibMonth.toFixed(2)}`);
    expect(rate("egress")).toContain(`$${prices.egressGib}`);
  });

  it("gives every plan the same usage lines, in the same order", () => {
    const order = pricing.usage.map((line) => line.id);
    for (const plan of pricing.plans) {
      expect(plan.usage.map((line) => line.id), plan.id).toEqual(order);
    }
  });
});
