import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { ContainerCircle } from "@/components/container-helix";
import { pageSections } from "@/lib/page";

// A proposal for a background: the yard's container in 3D, a chain of them
// turning behind the page and posed again as it is scrolled
// (container-helix.tsx). The sections are only there to scroll through.

export const metadata = { title: "Proposal · Container ring", robots: { index: false } };

export default function Page() {
  return (
    <>
    <ContainerCircle />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero
        variant="xlarge"
        heading="You built it. We ship it."
        description="A ring of containers around you, turning as you scroll: they come from far ahead, pass by at the sides and go on behind."
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
