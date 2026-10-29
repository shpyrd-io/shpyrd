import { gap } from "@/content/home";
import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { FeatureBlocks } from "@/components/feature-blocks";
import { FeatureGrid } from "@/components/feature-grid";
import { CompanyApps } from "@/components/company-apps";

// A test of the home's closing call (Giovani, 2026-10-20): the home as it is,
// but the last call on the orange band (CtaBand), the shipyard seen through
// the logo's bar, white, instead of multiplied into the orange.
export const metadata = { title: "Proposal · closing call in the bar" };

export default function Page() {
  return (
    <Home
      picture={<ShipStory />}
      features={
        <FeatureGrid heading={gap.heading} description={gap.description}>
          <FeatureBlocks className="mt-0" />
        </FeatureGrid>
      }
      order="conversion"
      companiesPicture={<CompanyApps />}
      closing="band"
    />
  );
}
