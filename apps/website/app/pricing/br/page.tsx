import { pricingFor } from "@shpyrd/content/site/pricing";
import { Pricing } from "@/components/pricing";
import { shared } from "@/lib/metadata";

// The pricing page in Brazil, with its own prices in reais. Visitors from
// Brazil are sent here from /pricing (src/lib/redirects.ts); search engines keep
// /pricing, so this one is not indexed.
const pricing = pricingFor("br");

export const metadata = {
  title: pricing.title,
  description: pricing.hero.description,
  robots: { index: false },
  ...shared(`${pricing.title} · shpyrd`, pricing.hero.description),
};

export default function Page() {
  return <Pricing region="br" />;
}
