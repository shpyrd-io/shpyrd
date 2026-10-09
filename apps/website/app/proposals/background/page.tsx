import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { ContainerHelix } from "@/components/container-helix";
import { pageSections } from "@/lib/page";

// A proposal for a background: the yard's container in 3D, a chain of them
// turning behind the page and posed again as it is scrolled
// (container-helix.tsx). The sections are only there to scroll through.

export const metadata = { title: "Proposal · Container background", robots: { index: false } };

export default function Page() {
  return (
    <>
    <ContainerHelix />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero
        variant="xlarge"
        heading="You built it. We ship it."
        description="A background of the yard's container, in 3D: a chain of them turning slowly behind the page, posed again as you scroll."
        actions={<Button size="lg">Get started</Button>}
      />
      {["Shipped", "Shared", "Kept running", "Handed over"].map((h) => (
        <section key={h} className="grid min-h-[80vh] place-items-center">
          <SectionIntro className="w-full" align="center" variant="xlarge" heading={h} description="A section to scroll through." />
        </section>
      ))}
    </PageLayoutContent>
    </>
  );
}
