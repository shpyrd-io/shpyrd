import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { LandingPage } from "@/components/landing";
import { ScrollReveal } from "@/components/scroll-reveal";
import { DeployStory } from "@/components/deploy-story";
import { HeroPrism } from "@/components/hero-prism";
import { page } from "./copy";

export const metadata = {
  title: "Proposal · Easy Deployment, rewritten",
  robots: { index: false },
};

// A test of 2026-11-12 (Giovani): /features/easy-deployment with the copy of
// the review (claude.ai/artifact/UGiENWZbViUFUfcrWpGwLv), benefit first for
// vibecoders and non-developers, and a hero that plays the three steps
// (deploy-story.tsx). The page's design, slug and sections as they are.
export default function Page() {
  return (
    <PageLayoutContent as="div" className="relative isolate overflow-x-clip">
      <ScrollReveal />
      <LandingPage page={page} picture={<DeployStory />} background={<HeroPrism icon="shpyrd" />} stepsGround="white" />
    </PageLayoutContent>
  );
}
