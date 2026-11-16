import { gap } from "@/content/home";
import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { FeatureBlocks } from "@/components/feature-blocks";
import { FeatureGrid } from "@/components/feature-grid";
import { CustomerStories } from "@/components/customer-stories";
import { CompanyApps } from "@/components/company-apps";

export const metadata = {
  title: "Proposal · Not the one who built it, as a list",
  robots: { index: false },
};

// A test of 2026-11-11 (Giovani): the home as it is, but "Not the one who
// built it?" with its heading on the left and the three ways as buttons on
// the right, with no films.
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
      also="list"
    />
  );
}
