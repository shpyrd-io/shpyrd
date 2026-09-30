import { Button } from "@shpyrd/ui/components/button";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { AddToAgent } from "@/components/add-to-agent";
import { SubpageStep } from "@/components/subpage-step";

// What the pages of the "Agents and workers" section share.

// The end of the section's own page: connect your agent, or read how processes
// are declared.
export function AgentsNext({
  heading = "Put the next one somewhere it keeps running",
}: {
  heading?: string;
}) {
  return (
    <Stack gap="normal" className="border-t pt-12">
      <SectionIntro
        heading={heading}
        description="Connect your agent once, then tell it what to run. Or read how processes work."
      />
      <Stack direction="horizontal" gap="cozy" className="flex-wrap">
        <AddToAgent />
        <Button variant="outline" asChild>
          <a href="/docs/deploying">Processes and deploys</a>
        </Button>
        <Button variant="ghost" asChild>
          <a href="/docs/logs">Logs</a>
        </Button>
      </Stack>
    </Stack>
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
