import { pricing } from "@shpyrd/content/site/pricing";
import { Pricing } from "@/components/pricing";

export const metadata = {
  title: pricing.title,
  description: pricing.hero.description,
};

export default function Page() {
  return <Pricing />;
}
