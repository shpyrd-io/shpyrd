import { Tour } from "@/components/tour-v3/tour";

// The Small Software manifesto: a tour of shpyrd as a deck, one slide a
// screen, for a company whose teams already build apps with AI. Linked from
// the home page (its companies section, the footer, the menu). It is the v3
// now, the working copy of /proposals/manifesto-v3 (Giovani, 2026-10-29: the
// home linked to the manifesto of now); the original stays in
// src/components/tour, shown at /proposals/manifesto-original.
export const metadata = {
  title: "The Small Software manifesto",
  description:
    "Welcome to the Small Software era: apps from every team, built with AI, running behind your company's sign-in on shpyrd.",
};

export default function SmallSoftwareManifesto() {
  return <Tour />;
}
