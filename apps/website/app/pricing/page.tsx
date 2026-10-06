import { pricing } from "@shpyrd/content/site/pricing";
import { Pricing } from "@/components/pricing";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: pricing.title,
  description: pricing.hero.description,
  ...shared(`${pricing.title} · shpyrd`, pricing.hero.description),
};

export default function Page() {
  return <Pricing />;
}
