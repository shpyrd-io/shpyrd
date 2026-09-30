import { Home } from "@/components/home";
import { ProposalNav } from "@/components/proposals";
import { ShippingChat } from "@/components/shipping-chat";

// Homepage proposal 5 - the chat. The same homepage as "/", with the two
// browser windows replaced by the conversation that ships the app: it shows
// the product the way the builder will use it, and names the same agent as
// the "Add to" button beside it.

export const metadata = { title: "Proposal 5 · The chat" };

export default function Page() {
  return <Home before={<ProposalNav current={5} />} picture={<ShippingChat />} />;
}
