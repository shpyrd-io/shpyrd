import { History, KeyRound, RefreshCw, ScrollText, SlidersHorizontal } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { AgentsNext, workerYaml } from "@/components/use-case-agents-and-workers";

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
  { icon: <History />, heading: "A bad change comes back", body: "Every deploy is a numbered release. Roll back to the one that worked." },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        label="Agents and workers"
        heading="Not everything you build has a web page."
        description="The agent that reads your inbox. The worker that turns uploads into reports. The bot in your team chat. shpyrd runs them too, on shpyrd cloud, the same way it runs apps."
        image={<IDE files={[{ name: "shpyrd.yaml", code: workerYaml, language: "yaml" }]} tabs showLineNumbers={false} />}
      />

      <Stack gap="spacious">
        <SectionIntro heading="What running it properly means" />
        <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-3">
          {kept.map((k) => (
            <Pillar key={k.heading} icon={k.icon} heading={k.heading} description={k.body} />
          ))}
        </div>
        <p className="max-w-prose text-sm text-muted-foreground">
          Not yet: running something on a schedule. Scheduled tasks are on the roadmap; today a
          worker runs all the time.
        </p>
      </Stack>

      <AgentsNext />
    </PageLayoutContent>
  );
}
