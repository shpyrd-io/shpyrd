import { Button } from "@shpyrd/ui/components/button";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SimpleCta } from "@/components/simple-cta";
import { pageSections } from "@/lib/page";
import { signUp } from "@/lib/signup";

export const metadata = {
  title: "Proposal · The simple CTA's light, still",
  robots: { index: false },
};

// A test of 2026-11-16 (Giovani): the simple CTA's light, which turns and
// shimmers at every frame, against the same light still, drawn once. The
// words are Easy Deployment's.
const cta = (still: boolean) => (
  <SimpleCta
    still={still}
    heading="Which app should we put online?"
    description="It's free to start. Deploy it today, and every change after."
    action={
      <Button size="lg" asChild>
        <a href={signUp}>Deploy your app for free</a>
      </Button>
    }
    links={[
      { label: "Apps Built with AI", href: "/solutions/apps-built-with-ai" },
      { label: "Websites & Landing Pages", href: "/solutions/websites" },
    ]}
  />
);

function Label({ children }: { children: React.ReactNode }) {
  return <p className="text-center font-mono text-xs tracking-wide text-muted-foreground uppercase">{children}</p>;
}

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <div className="grid gap-10">
        <Label>Now: the light animated</Label>
        {cta(false)}
      </div>
      <div className="grid gap-10 border-t border-foreground/8 pt-24 dark:border-foreground/20">
        <Label>Proposal: the same light, still</Label>
        {cta(true)}
      </div>
    </PageLayoutContent>
  );
}
