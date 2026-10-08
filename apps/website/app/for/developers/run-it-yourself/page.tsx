import { Building2, Cloud, Gauge, KeyRound, Laptop, ScrollText, Server, Settings2, Users } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { Boundaries } from "@/components/boundaries";
import { DeveloperStep } from "@/components/for-developers";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { CodeWindow } from "@/components/code-window";
import { infoTable } from "@/lib/info-table";
import { pageSections } from "@/lib/page";

// For developers - run it yourself, the last tab: shpyrd is open source, and
// the same platform runs on a Kubernetes cluster you control. Where it runs,
// what it is made of, the rules you set once, and what it doesn't do. Commands
// from docs/installation, docs/aws, docs/oracle-cloud, docs/access and docs/cli.

export const metadata = { title: "For developers · Run it yourself" };

const where = [
  {
    id: "laptop",
    label: "Your laptop",
    icon: <Laptop />,
    body: "A kind cluster in Docker (6–8 GB of memory, macOS or Linux), to try it and to develop against. 10–20 minutes the first time.",
    code: `brew install shpyrd-io/tap/shpyrd\nshpyrd cluster create\nshpyrd cluster trust-ca\nshpyrd cluster dashboard`,
  },
  {
    id: "aws",
    label: "AWS",
    icon: <Cloud />,
    body: "Amazon EKS, with the network, cluster and VPN from the Terraform in contrib/aws, then one command for the platform.",
    code: `shpyrd cluster init --context eks-shpyrd-prod --profile aws \\\n  --vars-file contrib/aws/terraform/shpyrd-prod.vars …`,
  },
  {
    id: "oci",
    label: "Oracle Cloud",
    icon: <Server />,
    body: "Oracle Kubernetes Engine, with the Terraform in contrib/oci, then the same command with its own profile.",
    code: `shpyrd cluster init --context oke-shpyrd-prod --profile oci \\\n  --vars-file contrib/oci/terraform/shpyrd-prod.vars …`,
  },
];

const parts = [
  { job: "Builds", part: "Cloud Native Buildpacks (kpack), or BuildKit for a Dockerfile" },
  { job: "Certificates", part: "cert-manager and Let's Encrypt" },
  { job: "Images", part: "A registry inside the cluster" },
  { job: "Databases", part: "CloudNativePG for PostgreSQL, Valkey or Redis for caches" },
  { job: "Metrics", part: "Prometheus and Grafana" },
  { job: "Roles", part: "Mirrored into Kubernetes RBAC, so kubectl sees what the dashboard does" },
];

const rules = [
  { icon: <Building2 />, what: "Your company's sign-in", code: "shpyrd sso add google --hosted-domain acme.com …" },
  { icon: <Users />, what: "Teams from your directory", code: "shpyrd teams create finance --group Finance" },
  { icon: <Gauge />, what: "What an app may use", code: "shpyrd sizes list" },
  { icon: <Settings2 />, what: "What every app gets", code: "shpyrd globals set SENTRY_DSN=…" },
  { icon: <ScrollText />, what: "Where logs go", code: "shpyrd drains add https://logs.acme.com/ingest --cluster" },
  { icon: <KeyRound />, what: "Who can do what", code: "shpyrd members add expenses --team finance --role user" },
];

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero align="center"
        variant="page"
        heading="Run it yourself."
        description="shpyrd is open source under MPL-2.0. The same platform, and the same commands, run on a Kubernetes cluster you control."
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="Where it runs" />
        <Tabs defaultValue="laptop" className="mx-auto w-full min-w-0 max-w-2xl">
          <TabsList>
            {where.map((w) => (
              <TabsTrigger key={w.id} value={w.id} icon={w.icon}>
                {w.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {where.map((w) => (
            <TabsContent key={w.id} value={w.id} className="grid gap-3 pt-4">
              <p className="text-muted-foreground">{w.body}</p>
              <IDE code={w.code} language="sh" showLineNumbers={false} />
            </TabsContent>
          ))}
        </Tabs>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="What it's made of"
          description="The tools you would have picked yourself, wired together once. A project is a namespace, and the base stack can be rendered to plain manifests."
        />
        <Card className={cn(glass, infoTable, "overflow-x-auto p-0")}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-1/4">For</TableHead>
                <TableHead>shpyrd uses</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {parts.map((p) => (
                <TableRow key={p.job}>
                  <TableCell className="font-medium">{p.job}</TableCell>
                  <TableCell className="whitespace-normal text-muted-foreground">{p.part}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
        <CodeWindow
          className="mx-auto w-full max-w-2xl"
          title="Terminal"
          code={`kubectl -n app-shop get pods\nshpyrd cluster export -o manifests/`}
          language="sh"
        />
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="The rules you set once"
          description="On your own cluster the platform's settings are yours. Each is a command, and the same setting in the dashboard."
        />
        <ul className="grid gap-x-8 gap-y-5 md:grid-cols-2">
          {rules.map((r) => (
            <li key={r.what} className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 [&_svg]:size-5">
              <span className="row-span-2 mt-0.5 text-muted-foreground">{r.icon}</span>
              <p className="font-heading font-medium">{r.what}</p>
              <code className="truncate font-mono text-xs text-muted-foreground">{r.code}</code>
            </li>
          ))}
        </ul>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="What it doesn't do" description="Worth knowing before you spend an afternoon on it." />
        <Boundaries items={boundaries.filter((b) => ["compatibility", "rollback", "audit", "licence"].includes(b.id))} />
      </Stack>

      <DeveloperStep
        next={{ href: "/for/developers", label: "Overview" }}
        action={{ href: "https://github.com/shpyrd-io/shpyrd", label: "Read the source" }}
      />
    </PageLayoutContent>
  );
}
