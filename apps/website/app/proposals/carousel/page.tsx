import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { FeatureBlocks } from "@/components/feature-blocks";
import { FeatureCarousel } from "@/components/feature-carousel";
import { gap } from "@/content/home";
import { CustomerStories } from "@/components/customer-stories";
import { CompanyApps } from "@/components/company-apps";

// A proposal for the home's features (2026-10-19): the eight feature blocks
// as a carousel of cards, after stripe.com/br/payments.
export const metadata = { title: "Proposal · features carousel" };

export default function Page() {
  return (
    <Home
      picture={<ShipStory />}
      features={
        <FeatureCarousel heading={gap.heading} description={gap.description}>
          <FeatureBlocks carousel />
        </FeatureCarousel>
      }
      stories={<CustomerStories />}
      order="conversion"
      companiesPicture={<CompanyApps />}
    />
  );
}
