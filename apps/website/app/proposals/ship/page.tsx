import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";

// A proposal for the home's picture (2026-10-16): one chat, the agents it
// works with above it, the app building itself and flying up to the cloud.
export const metadata = { title: "Proposal · the chat that ships" };

export default function Page() {
  return <Home picture={<ShipStory />} />;
}
