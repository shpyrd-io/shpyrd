import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { document } from "@/lib/mdx/catalog";
import { EditorialLanding } from "@/components/editorial-landing";
import { ScrollReveal } from "@/components/scroll-reveal";

export const metadata = {
  title: "Proposal · The hero mark on the hero middle",
  robots: { index: false },
};

// A test of 2026-11-16 (Giovani). The hero's 128px above and below is now
// every inner page's (landing.tsx). Here: the shpyrd mark behind the hero,
// where it was across, its middle on the hero's middle top to bottom
// (hero-prism.tsx, [data-prism-middle]); its digits a quarter larger than
// the manifesto's ([data-prism-grain]).
export default async function Page() {
  const page = await document("features", "easy-deployment");
  if (!page) notFound();
  return (
    <PageLayoutContent as="div">
      <ScrollReveal />
      <page.default components={{ EditorialLanding }} />
    </PageLayoutContent>
  );
}
