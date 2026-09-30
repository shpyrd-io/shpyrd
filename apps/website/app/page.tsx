import { hero } from "@shpyrd/content/site/home";
import { Home } from "@/components/home";
import { ShippingChat } from "@/components/shipping-chat";

export const metadata = {
  title: { absolute: `shpyrd - ${hero.heading}` },
  description: hero.description,
};

// The picture beside the heading is the chat that ships the app (homepage
// proposal 5, chosen 2026-09-30): the product shown the way the builder will
// use it, naming the same agent as the "Add to" button beside it.
export default function Page() {
  return <Home picture={<ShippingChat />} />;
}
