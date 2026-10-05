import { pricingFor } from "@shpyrd/content/site/pricing";
import { Pricing } from "@/components/pricing";

// The pricing page in Brazil, with its own prices in reais. Visitors from
// Brazil are sent here from /pricing (vercel.json); search engines keep
// /pricing, so this one is not indexed.
const pricing = pricingFor("br");

export const metadata = {
  title: pricing.title,
  description: pricing.hero.description,
  robots: { index: false },
};

export default function Page() {
  return <Pricing region="br" />;
}
