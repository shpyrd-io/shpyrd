import Link from "next/link";
import { Button } from "@shpyrd/ui/components/button";
import { CtaBand } from "@shpyrd/ui/components/cta-band";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ShipyardBar } from "@/components/shipyard-bar";
import { price } from "@/content/home";
import { start } from "@/content/offer";
import { signUp } from "@/lib/signup";
import { pageSections } from "@/lib/page";

export const metadata = {
  title: "Proposal · The home's closing band, its light still",
  robots: { index: false },
};

// A test of 2026-11-16 (Giovani): the home's closing band (home.tsx,
// closing="band") with its light animated, as now, over the same band with
// the same light still (CtaBand still).
const band = (still: boolean) => (
  <div className="relative isolate mx-[calc(50%-50vw)] overflow-x-clip px-[calc(50vw-50%)] before:pointer-events-none before:absolute before:inset-x-0 before:top-0 before:z-30 before:h-px before:bg-foreground/8 after:pointer-events-none after:absolute after:inset-x-0 after:-bottom-[23px] after:z-30 after:h-px after:bg-foreground/8 dark:before:bg-foreground/20 dark:after:bg-foreground/20">
    <CtaBand
      still={still}
      className="[&_[data-slot=hero-image]]:mix-blend-normal! lg:[&_[data-slot=hero-heading]]:text-6xl! [&_.cta-band-hero>div>div:first-child]:pb-14!"
      heading="Start on shpyrd cloud"
      description="Sign up, connect your agent, and ship your first app. Or run it yourself: shpyrd is open source under MPL-2.0, and the quick start takes you from nothing to a deployed app."
      actions={
        <>
          <Button size="lg" asChild>
            <a href={signUp}>Get started</a>
          </Button>
          <Button variant="link" size="lg" asChild>
            <a href="/docs/installation">Run it yourself</a>
          </Button>
        </>
      }
      note={
        <>
          {start.note}{" "}
          <Link href={price.link.href} className="font-medium underline underline-offset-4">
            {price.link.label} →
          </Link>
        </>
      }
      image={<ShipyardBar />}
    />
  </div>
);

function Label({ children }: { children: React.ReactNode }) {
  return <p className="text-center font-mono text-xs tracking-wide text-muted-foreground uppercase">{children}</p>;
}

// ?only=now or ?only=still shows one band alone, to measure it.
export default async function Page({ searchParams }: { searchParams: Promise<{ only?: string }> }) {
  const { only } = await searchParams;
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      {only !== "still" && <Label>Now: the light animated</Label>}
      {only !== "still" && band(false)}
      {only !== "now" && <Label>Proposal: the same light, still</Label>}
      {only !== "now" && band(true)}
    </PageLayoutContent>
  );
}
