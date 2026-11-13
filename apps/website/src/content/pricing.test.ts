import { describe, expect, it } from "vitest";
import { hoursPerMonth, lineCost, money, pricing, pricingFor, regions } from "./pricing";

const exampleTotal = (page: ReturnType<typeof pricingFor>) =>
  page.example.lines.reduce((sum, line) => sum + lineCost(page.region.rates.starter, line), 0);

describe("the pricing page", () => {
  it("costs each line of the example from Starter's rates", () => {
    const starter = regions.international.rates.starter;
    expect(lineCost(starter, { cpuCores: 1, hours: hoursPerMonth })).toBeCloseTo(20.44);
    expect(lineCost(starter, { dedicatedCpuCores: 1, hours: hoursPerMonth })).toBeCloseTo(25.55);
    expect(lineCost(starter, { memoryGib: 1, hours: hoursPerMonth })).toBeCloseTo(6.497);
    // A 5 GiB disk is billed as the 16 GiB minimum; a 20 GiB disk as 20.
    expect(lineCost(starter, { storageGib: 5 })).toBeCloseTo(2.96);
    expect(lineCost(starter, { storageGib: 20 })).toBeCloseTo(3.7);
    expect(lineCost(starter, { egressGib: 10 })).toBeCloseTo(0.5);
    expect(lineCost(starter, {})).toBe(0);
  });

  it("comes to more than the minimum for the example in USD, so the usage is what is paid", () => {
    const total = exampleTotal(pricing);
    expect(total).toBeCloseTo(5.61, 2);
    expect(Math.max(total, pricing.example.minimum)).toBe(total);
  });

  it("comes to more than the minimum for the example in Brazil too, so the usage is what is paid", () => {
    const br = pricingFor("br");
    const total = exampleTotal(br);
    expect(total).toBeCloseTo(30.3, 2);
    expect(Math.max(total, br.example.minimum)).toBe(total);
  });

  it("writes amounts as each region does", () => {
    expect(money(regions.international, 0.09)).toBe("$0.09");
    expect(money(regions.international, 7.157, 2)).toBe("$7.16");
    expect(money(regions.br, 0.486)).toBe("R$\u00a00,486");
    expect(money(regions.br, 27, 2)).toBe("R$\u00a027,00");
    expect(money(regions.international, 1500)).toBe("$1,500");
    expect(money(regions.br, 1344.6, 2)).toBe("R$\u00a01.344,60");
  });

  it("shows Brazil's own prices, not converted ones", () => {
    const br = pricingFor("br");
    const plan = (id: string) => br.plans.find((p) => p.id === id)!;
    expect(plan("starter").price).toBe("25");
    expect(plan("pro").price).toBe("199");
    expect(plan("starter").usage.map((u) => u.value)).toEqual(["R$\u00a00,15", "R$\u00a00,19", "R$\u00a00,048", "R$\u00a01", "R$\u00a00,27"]);
    expect(plan("pro").usage.map((u) => u.value)).toEqual(["R$\u00a00,13", "R$\u00a00,17", "R$\u00a00,04", "R$\u00a00,95", "R$\u00a00,216"]);
    expect(plan("business").price).toBe("2.499");
    expect(plan("business").usage.map((u) => u.value)).toEqual(["R$\u00a00,11", "R$\u00a00,15", "R$\u00a00,033", "R$\u00a00,9", "R$\u00a00,189"]);
  });

  it("shows each paid plan's rates as the prices it charges, in every region", () => {
    for (const region of Object.values(regions)) {
      const page = pricingFor(region.id);
      for (const id of ["starter", "pro", "business"] as const) {
        const plan = page.plans.find((p) => p.id === id)!;
        const shown = (line: string) => plan.usage.find((u) => u.id === line)?.value;
        const r = region.rates[id];
        expect(shown("cpu"), `${region.id} ${id}`).toBe(money(region, r.cpuCoreHour));
        expect(shown("dedicatedCpu"), `${region.id} ${id}`).toBe(money(region, r.dedicatedCpuCoreHour));
        expect(shown("memory"), `${region.id} ${id}`).toBe(money(region, r.memoryGibHour));
        expect(shown("storage"), `${region.id} ${id}`).toBe(money(region, r.storageGibMonth));
        expect(shown("egress"), `${region.id} ${id}`).toBe(money(region, r.egressGib));
      }
    }
  });

  it("keeps every paid plan's rates above cost, and the cheaper plan never above the dearer one", () => {
    // Loaded cost on prod, 2026-11 (shpyrd-cloud#70): a GiB of memory is
    // about R$8.21 a month, a held core R$57; the floor is 1.5 times that.
    const br = regions.br.rates;
    for (const plan of ["starter", "pro", "business"] as const) {
      expect(br[plan].memoryGibHour * hoursPerMonth, plan).toBeGreaterThan(8.21 * 1.5);
      expect(br[plan].dedicatedCpuCoreHour * hoursPerMonth, plan).toBeGreaterThan(57 * 1.5);
      expect(br[plan].storageMinimumGib).toBe(16);
    }
    for (const region of Object.values(regions)) {
      for (const key of ["cpuCoreHour", "dedicatedCpuCoreHour", "memoryGibHour", "storageGibMonth", "egressGib"] as const) {
        expect(region.rates.pro[key], `${region.id} ${key}`).toBeLessThanOrEqual(region.rates.starter[key]);
        expect(region.rates.business[key], `${region.id} ${key}`).toBeLessThanOrEqual(region.rates.pro[key]);
      }
    }
  });

  it("says when a paid plan is invoiced early only where that amount was given", () => {
    const footnote = (page: ReturnType<typeof pricingFor>) => page.plans.find((p) => p.id === "starter")!.footnote;
    expect(footnote(pricing)).toContain("Invoiced early past $25");
    expect(pricing.plans.find((p) => p.id === "business")!.footnote).toBe("No ceilings. Invoiced early past $1,500 owed at first.");
    expect(footnote(pricingFor("br"))).not.toContain("Invoiced early");
  });

  it("links each region's page to the other's", () => {
    expect(pricing.footnote.other.href).toBe("/pricing/br");
    // The USD page from Brazil: the query is what the redirect lets through.
    expect(pricingFor("br").footnote.other.href).toBe("/pricing?currency=usd");
  });

  it("gives every plan the same usage lines, in the same order", () => {
    const order = pricing.usage.map((line) => line.id);
    for (const plan of pricing.plans) {
      expect(plan.usage.map((line) => line.id), plan.id).toEqual(order);
    }
  });

  it("starts every plan with the Add to button; only Enterprise is a conversation", () => {
    for (const plan of pricing.plans) {
      expect(plan.action, plan.id).toEqual({ label: "Add to", kind: "install" });
    }
  });

  it("keeps Enterprise out of the priced plans: a conversation, not a price", () => {
    expect(pricing.plans.map((p) => p.id)).toEqual(["free", "starter", "pro", "business"]);
    expect(pricing.enterprise).not.toHaveProperty("price");
    expect(pricing.enterprise).not.toHaveProperty("usage");
    expect(pricing.enterprise.action).toEqual({ label: "Contact us", kind: "contact" });
  });

  it("names one point of Enterprise for each licensed feature of ee/", () => {
    expect(pricing.enterprise.features).toHaveLength(4);
  });
});
