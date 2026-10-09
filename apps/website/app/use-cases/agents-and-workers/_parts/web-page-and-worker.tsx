import { ArrowRight, Cog, FileCode, Globe } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { webAndWorkerYaml } from "@/components/use-case-agents-and-workers";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { CodeWindow } from "@/components/code-window";
import { pageSections } from "@/lib/page";

// Agents and workers · Page and worker. One job: the app that grew a part that
// works in the background. One app, both halves: one deploy, one set of
// settings, each half scaled on its own.

function Half({ icon, title, body }: { icon: React.ReactNode; title: string; body: string }) {
  return (
    <Card className={cn(glass, "gap-2 px-5 [&_svg]:size-5")}>
      <span className="text-muted-foreground">{icon}</span>
      <p className="font-heading font-medium">{title}</p>
      <p className="text-sm text-muted-foreground">{body}</p>
    </Card>
  );
}

export function Part() {
  return (
    <>
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2"
        variant="page"
        heading="The page people use, and the worker behind it."
        description="Your purchase tracker has a page for Finance and a worker that chases approvals by email. Two halves, one app: one deploy, one set of settings."
        image={<CodeWindow title="shpyrd.yaml" icon={<FileCode />} code={webAndWorkerYaml} language="yaml" roomy />}
      />

      <div className="grid items-stretch gap-3 md:grid-cols-[1fr_auto_1fr]">
        <Half icon={<Globe />} title="web" body="Gets the address and the sign-in. Finance opens it." />
        <ArrowRight aria-hidden className="size-5 self-center justify-self-center text-muted-foreground max-md:rotate-90" />
        <Half icon={<Cog />} title="worker" body="No address, nobody opens it. It works through the queue." />
      </div>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="What they share, and what they don't" />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar variant="card" heading="One deploy" description="Both halves ship together, as one numbered release. Rolling back takes both back." />
          <Pillar variant="card" heading="The same settings" description="Config vars and an attached database reach both: the same DATABASE_URL on each side." />
          <Pillar variant="card" heading="Scaled apart" description="Three of the page when Finance closes the month, one worker all year: shpyrd scale web=3 worker=1." />
        </div>
      </Stack>

    </PageLayoutContent>
    </>
  );
}
