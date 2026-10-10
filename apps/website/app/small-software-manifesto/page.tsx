import { Tour } from "@/components/tour/tour";

// The Small Software manifesto: a tour of shpyrd as a deck, one slide a
// screen, for a company whose teams already build apps with AI. Linked from
// the home page's footer.
export const metadata = {
  title: "The Small Software manifesto",
  description: "Welcome to the Small Software era: apps from every team, built with AI, running behind your company's sign-in on shpyrd.",
};

export default function SmallSoftwareManifesto() {
  return <Tour />;
}
