import {
  Bot,
  Database,
  GitBranch,
  Globe,
  HardDrive,
  History,
  KeyRound,
  LineChart,
  Moon,
  Package,
  ScrollText,
  ShieldCheck,
  SlidersHorizontal,
  Workflow,
} from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { pageSections } from "@/lib/page";

// For developers - what's included: what a developer checks before choosing,
// on every workspace. Each line maps to docs that say it ships: deploying,
// shpyrd-yaml, databases, resources, logs, domains, app-access, access, mcp,
// and the roadmap for sleep (RFC-0075). The AI assistant reads; it does not
// write yet.

type Item = { icon: React.ReactElement; heading: string; body: string };

const groups: { heading: string; items: Item[] }[] = [
  {
    heading: "Building and shipping",
    items: [
      {
        icon: <Package />,
        heading: "Buildpacks, or your Dockerfile",
        body: "Go, Node.js, Python, Ruby, Java, .NET and static sites build with no Dockerfile. When there is one, it is used.",
      },
      {
        icon: <GitBranch />,
        heading: "From a folder, Git or an image",
        body: "Deploy your checkout, a Git URL that rebuilds on new commits, or an image you already built.",
      },
      {
        icon: <History />,
        heading: "Releases and rollback",
        body: "Every deploy and config change is a numbered release. Rolling back restores the build and its config together.",
      },
      {
        icon: <Workflow />,
        heading: "A release phase",
        body: "A release process runs before the new version takes over, for migrations. If it fails, the previous release keeps serving.",
      },
    ],
  },
  {
    heading: "Running",
    items: [
      {
        icon: <SlidersHorizontal />,
        heading: "Processes and config vars",
        body: "Web and worker processes, each scaled on its own. Config vars you set once and never see again.",
      },
      {
        icon: <Database />,
        heading: "Postgres and Redis",
        body: "Create one, attach it, and DATABASE_URL or REDIS_URL is in the app.",
      },
      {
        icon: <HardDrive />,
        heading: "Volumes",
        body: "Disks that survive deploys, for what an app keeps on its own.",
      },
      {
        icon: <Moon />,
        heading: "Sleep when nobody uses it",
        body: "An idle app can sleep and wakes on the next request, in about 6–7 seconds; a sleeping Postgres wakes in about 36.",
      },
    ],
  },
  {
    heading: "Watching and reaching it",
    items: [
      {
        icon: <ScrollText />,
        heading: "Logs, and drains",
        body: "Live logs named by instance, and drains that forward them to your provider.",
      },
      {
        icon: <LineChart />,
        heading: "Metrics",
        body: "Throughput by status, response time percentiles, CPU and memory against what each process has.",
      },
      {
        icon: <Globe />,
        heading: "Custom domains",
        body: "Serve an app at a name you own, with its certificate issued for you.",
      },
    ],
  },
  {
    heading: "Access",
    items: [
      {
        icon: <ShieldCheck />,
        heading: "Sign-in in front of every app",
        body: "New apps ask visitors to sign in, and your app is told who they are in headers. No sign-in code of your own.",
      },
      {
        icon: <KeyRound />,
        heading: "API tokens",
        body: "Personal tokens for CI and scripts, never above the role of who made them, revoked at once.",
      },
      {
        icon: <Bot />,
        heading: "An AI assistant connection",
        body: "Every workspace is a remote MCP server: ask Claude about your projects, logs and metrics. It reads today.",
      },
    ],
  },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero as="h2" align="center"
        variant="page"
        heading="What's included."
        description="What you would check before choosing, on every workspace."
      />

      {groups.map((g) => (
        <Stack key={g.heading} gap="spacious">
          <SectionIntro align="center" variant="xlarge" heading={g.heading} />
          <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
            {g.items.map((i) => (
              <Pillar variant="card" key={i.heading} icon={i.icon} heading={i.heading} description={i.body} />
            ))}
          </div>
        </Stack>
      ))}

    </PageLayoutContent>
  );
}
