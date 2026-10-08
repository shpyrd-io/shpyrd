import { FileCode, History, KeyRound, RefreshCw, ScrollText, SlidersHorizontal } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { AgentsNext, workerYaml } from "@/components/use-case-agents-and-workers";
import { CodeWindow } from "@/components/code-window";
import { BinaryMark } from "@/components/binary-mark";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { SubpageStep } from "@/components/subpage-step";
import { pageSections } from "@/lib/page";

// Agents and workers, the section's own page: what you built with AI doesn't
// need a web page to be run properly, what "properly" means, and the one thing
// that isn't there yet (a schedule). Its subpages each take one part further.

export const metadata = {
  title: "Agents and workers",
  description:
    "The agent that reads your inbox, the worker behind a queue: things without a web page, kept running, with their secrets, logs and releases.",
};

const kept = [
  { icon: <RefreshCw />, heading: "It keeps running", body: "When it crashes, it starts again. When your laptop closes, nothing stops." },
  { icon: <KeyRound />, heading: "Its keys stay secret", body: "API keys are config vars: set once, never shown again, in the process's environment." },
  { icon: <ScrollText />, heading: "You can see what it did", body: "Its output is its log, named by instance (worker.1), live or sent where you keep logs." },
  { icon: <SlidersHorizontal />, heading: "Run one, or five", body: "Scale it like anything else. The count survives the next deploy." },
  { icon: <History />, heading: "Roll back a bad change", body: "Every deploy is a numbered release. Roll back to the one that worked." },
];

export default function Page() {
  return (
    <>
    {/* The page's background: the shpyrd mark made of falling binary
        (binary-mark.tsx), 100px under the top of the site, centred on the
        hero's code window across, reaching
        into the block after it, scrolling with the page as the home page's
        ocean does. Outside the page's grid. */}
    {/* The same size and place on every page of the section: 847px tall,
        its top 100px down, its centre 370px right of the page's middle. */}
    <BinaryMark height={847} offset={100} x={370} />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero variant="page"
        label="Agents and workers"
        heading="Not everything you build has a web page."
        description="The agent that reads your inbox. The worker that turns uploads into reports. The bot in your team chat. shpyrd runs them too, on shpyrd cloud, the same way it runs apps."
        image={<CodeWindow title="shpyrd.yaml" icon={<FileCode />} code={workerYaml} language="yaml" roomy />}
      />

      {/* The binary mark behind the page reaches down to here. */}
      <Stack gap="spacious" data-binary-end>
        <SectionIntro align="center" variant="xlarge" heading="What running it properly means" />
        {/* Trying (2026-10-08): the five side by side, each a glass card with
            its icon large over its name, then its line. */}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
          {kept.map((k) => (
            <Pillar
              key={k.heading}
              icon={k.icon}
              heading={k.heading}
              description={k.body}
              className={cn(
                glass,
                "gap-3 p-6 [&_[data-slot=pillar-description]]:text-sm [&_[data-slot=pillar-heading]]:text-base [&_[data-slot=pillar-icon]_svg]:!size-8 [&_[data-slot=pillar-icon]]:mb-2",
              )}
            />
          ))}
        </div>
        <p className="mx-auto max-w-prose text-center text-sm text-muted-foreground">
          Not yet: running something on a schedule. Scheduled tasks are on the roadmap; today a
          worker runs all the time.
        </p>
      </Stack>

      <AgentsNext />

      {/* As every page of the section ends: the way to its next one. */}
      <SubpageStep next={{ href: "/use-cases/agents-and-workers/move-it-off-your-laptop", title: "Move it off your laptop" }} />
    </PageLayoutContent>
    </>
  );
}
