import { gap, hero } from "@/content/home";
import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { FeatureBlocks } from "@/components/feature-blocks";
import { FeatureGrid } from "@/components/feature-grid";
import { CustomerStories } from "@/components/customer-stories";
import { CompanyApps } from "@/components/company-apps";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: { absolute: `shpyrd - ${hero.heading}` },
  description: hero.description,
  ...shared(`shpyrd - ${hero.heading}`, hero.description),
};

// The picture beside the heading is the chat that ships the app (chosen
// 2026-10-16, after proposal 5): one chat with "your favorite AI", the app
// building itself behind it and flying up into the shpyrd cloud. The rest is
// the design session of 2026-10-19, first tried on /proposals/carousel: the
// sections in the order that converts, the features as a carousel, the
// customer stories, the picture for businesses and the orange closing band.
// The closing call is the orange band with the yard in the logo's bar
// (tried on /proposals/cta-bar, applied 2026-10-20).
export default function Page() {
  return (
    <Home
      picture={<ShipStory />}
      features={
        <FeatureGrid heading={gap.heading} description={gap.description}>
          <FeatureBlocks className="mt-0" />
        </FeatureGrid>
      }
      stories={<CustomerStories />}
      order="conversion"
      companiesPicture={<CompanyApps />}
      closing="band"
    />
  );
}
