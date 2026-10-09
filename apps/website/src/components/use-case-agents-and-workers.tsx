import { DeployButton } from "@/components/deploy-button";
import { SubpageStep } from "@/components/subpage-step";
import { SimpleCta } from "@/components/simple-cta";

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
    <SimpleCta
      heading={heading}
      description="Connect your agent once, then tell it what to run. Or read how processes work."
      action={<DeployButton size="lg" />}
      links={[
        { label: "Processes and deploys", href: "/docs/deploying" },
        { label: "Logs", href: "/docs/logs" },
      ]}
    />
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
