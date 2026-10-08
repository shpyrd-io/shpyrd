import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { AddToAgent } from "@/components/add-to-agent";
import { SubpageStep } from "@/components/subpage-step";

// What the pages of the "Agents and workers" section share.

// The end of the section's own page: the words on the CTA block's light
// ("brilho na água", src/styles/aurora.css), centred on them; the one thing to
// do in the middle, in orange; where to read on, as two plain links further
// down, near the footer (48px from its line, as a subpage's ending is).
export function AgentsNext({
  heading = "Put the next one somewhere it keeps running",
}: {
  heading?: string;
}) {
  return (
    <div data-slot="next-step" className="relative isolate grid justify-items-center gap-24 overflow-x-clip pb-6">
      {/* The light under the words and the button, not the links. */}
      <Stack gap="spacious" className="relative isolate w-full justify-items-center">
        <div aria-hidden="true" className="aurora aurora-centred">
          <span className="light" />
          <span className="light" />
          <span className="light" />
          <span className="light" />
          <span className="rays" />
        </div>
        <SectionIntro
          align="center"
          variant="xlarge"
          className="w-full [&_[data-slot=section-intro-description]]:text-muted-foreground dark:[&_[data-slot=section-intro-description]]:text-foreground/85"
          heading={heading}
          description="Connect your agent once, then tell it what to run. Or read how processes work."
        />
        <div className="mt-8 flex w-full justify-center [&>*]:items-center">
          <AddToAgent size="lg" />
        </div>
      </Stack>
      <nav aria-label="Read on" className="flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm">
        <a href="/docs/deploying" className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
          Processes and deploys
        </a>
        <a href="/docs/logs" className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
          Logs
        </a>
      </nav>
    </div>
  );
}

// The end of a subpage: where to read on in the section, and at most one thing
// to do. The full ending is the section's own page's.
export function NextSubpage({
  href,
  title,
  action,
}: {
  href?: string;
  title?: string;
  action?: React.ReactNode;
}) {
  return <SubpageStep action={action} next={href && title ? { href, title } : undefined} />;
}

// A worker next to a web process, as shpyrd.yaml declares it (docs/shpyrd-yaml).
export const workerYaml = `project: inbox-agent
processes:
  worker:
    command: ["python", "agent.py"]`;

export const webAndWorkerYaml = `project: purchase-requests
processes:
  web:
    port: 8080
  worker:
    command: ["node", "worker.js"]`;
