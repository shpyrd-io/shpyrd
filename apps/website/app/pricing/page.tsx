import { pricing } from "@/content/pricing";
import { Pricing } from "@/components/pricing";
import { shared } from "@/lib/metadata";
import { ScrollReveal } from "@/components/scroll-reveal";

export const metadata = {
  title: pricing.title,
  description: pricing.hero.description,
  ...shared(`${pricing.title} · shpyrd`, pricing.hero.description),
};

export default function Page() {
  // The page's blocks rise in as they come into view, as on the home.
  return (
    <>
      <ScrollReveal />
      <Pricing />
    </>
  );
}
