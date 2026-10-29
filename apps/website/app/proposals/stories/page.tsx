import { Home } from "@/components/home";
import { ShipStory } from "@/components/ship-story";
import { CustomerStories } from "@/components/customer-stories";

// A proposal for the home's testimonials (2026-10-19): one customer story at a
// time in a card of the customer's colour, after stripe.com/br/payments/ai.
export const metadata = { title: "Proposal · customer stories" };

export default function Page() {
  return <Home picture={<ShipStory />} stories={<CustomerStories />} />;
}
