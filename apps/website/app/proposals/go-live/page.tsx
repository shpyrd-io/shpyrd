import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { GoLive } from "@/components/go-live";

// A proposal for the home's features (2026-10-16): the eight blocks and the
// three questions as one statement and five things in a row, after neon.com.
export const metadata = { title: "Proposal · features in one row" };

export default function Page() {
  return <Home picture={<ShipStory />} features={<GoLive />} />;
}
