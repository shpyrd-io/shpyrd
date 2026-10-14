import { copy as plansCopy } from "@shpyrd/content/site/pricing/plans.mdx";
import { copy as internationalCopy } from "@shpyrd/content/site/pricing/international.mdx";
import { copy as brazilCopy } from "@shpyrd/content/site/pricing/br.mdx";
import type { PricingCopy } from "./contracts";
import { interpolate } from "./interpolate";

export const hoursPerMonth = 730;
type Rates = { cpuCoreHour: number; memoryGibHour: number; storageGibMonth: number; egressGib: number };
type PaidPlan = "starter" | "pro" | "business";
export type Region = {
  id: "international" | "br";
  currency: "USD" | "BRL";
  currencyName: string;
  symbol: "$" | "R$";
  minimum: Record<PaidPlan, number>;
  rates: Record<PaidPlan, Rates>;
  invoicedEarly?: Record<PaidPlan, number>;
};
export const regions: Record<Region["id"], Region> = {
  international: (internationalCopy as { region: Region }).region,
  br: (brazilCopy as { region: Region }).region,
};
const copy = (plansCopy as PricingCopy).pricing;
function number(region: Region, n: number, digits: number): string {
  return n.toLocaleString(region.currency === "BRL" ? "pt-BR" : "en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits });
}
export function money(region: Region, n: number, digits?: number): string {
  const text = number(region, n, digits ?? (String(n).split(".")[1]?.length ?? 0));
  return region.currency === "BRL" ? `R$\u00a0${text}` : `$${text}`;
}

export function pricingFor(id: Region["id"]) {
  const region = regions[id];
  const starter = region.rates.starter;
  const variables = {
    starterMinimum: money(region, region.minimum.starter),
    proMinimum: money(region, region.minimum.pro),
    businessMinimum: money(region, region.minimum.business),
    cpuMonth: money(region, starter.cpuCoreHour * hoursPerMonth, 2),
    memoryMonth: money(region, starter.memoryGibHour * hoursPerMonth, 2),
    databaseMonth: money(region, starter.storageGibMonth * 5, 2),
    egressExample: money(region, starter.egressGib * 10, 2),
    currencyName: region.currencyName,
  };
  // Expand only page text. The early-invoice template has its own amount below.
  const { earlyInvoice, ...page } = copy;
  const words = interpolate(page, variables);
  return {
    ...words,
    region,
    plans: words.plans.map((plan) => {
      const paid = plan.id as PaidPlan;
      const minimum = plan.id === "free" ? 0 : region.minimum[paid];
      const rates = region.rates[paid];
      const early = region.invoicedEarly?.[paid];
      return {
        ...plan,
        price: number(region, minimum, Number.isInteger(minimum) ? 0 : 2),
        usage: words.usage.map(({ id }) => ({ id, value: plan.id === "free" ? words.included : money(region, rates[{
          cpu: "cpuCoreHour", memory: "memoryGibHour", storage: "storageGibMonth", egress: "egressGib",
        }[id] as keyof Rates]) })),
        footnote: plan.footnote + (early === undefined ? "" : ` ${interpolate(earlyInvoice, { amount: money(region, early) })}`),
        note: undefined as string | undefined,
      };
    }),
    footnote: words.footnotes[id],
    example: { ...words.example, minimum: region.minimum.starter },
  };
}
export const pricing = pricingFor("international");

export function lineCost(rates: Rates, line: { cpuCores?: number; memoryGib?: number; hours?: number; storageGib?: number; egressGib?: number }): number {
  const hours = line.hours ?? 0;
  return (line.cpuCores ?? 0) * hours * rates.cpuCoreHour
    + (line.memoryGib ?? 0) * hours * rates.memoryGibHour
    + (line.storageGib ?? 0) * rates.storageGibMonth
    + (line.egressGib ?? 0) * rates.egressGib;
}
