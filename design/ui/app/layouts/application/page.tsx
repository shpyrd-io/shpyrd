"use client";

import Link from "next/link";
import {
  ExternalLink,
  Globe,
  History,
  Plus,
  RotateCcw,
  Rocket,
  Settings2,
  Trash2,
  Upload,
} from "lucide-react";
import { toast } from "sonner";
import {
  Alert,
  AlertActions,
  AlertDescription,
  AlertTitle,
} from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { Button } from "@shpyrd/ui/components/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { DropdownButton } from "@shpyrd/ui/components/dropdown-button";
import { DropdownMenuItem } from "@shpyrd/ui/components/dropdown-menu";
import { Field } from "@shpyrd/ui/components/field";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Toaster } from "@shpyrd/ui/components/sonner";
import { Stack } from "@shpyrd/ui/components/stack";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { Section } from "../../section";
import { Frame } from "../frame";

// A whole application, as the console and the workspace application draw
// one, made only of the library: the frame around every screen, the list
// of what there is, one thing in its tabs, the screen while it loads and
// when it could not, and the settings. What it shows is made up.

export default function Page() {
  return (
    <>
      <Toaster position="bottom-right" />
      <Section title="The frame, and the list of what there is">
        <Frame here="projects">
          <Projects />
        </Frame>
      </Section>
      <Section title="One thing, in its tabs">
        <Frame here="projects" crumb="Hello World">
          <Project />
        </Frame>
      </Section>
      <Section title="While it loads, and when it could not">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Card>
            <CardContent className="grid gap-4">
              <Skeleton className="h-7 w-56" />
              <Skeleton className="h-4 w-80" />
              <Skeleton className="h-32 w-full" />
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <Alert variant="destructive">
                <AlertTitle>Could not load the project</AlertTitle>
                <AlertDescription>The server answered 502 at 17:04.</AlertDescription>
                <AlertActions>
                  <Button size="sm" variant="outline" icon={<RotateCcw />}>
                    Try again
                  </Button>
                </AlertActions>
              </Alert>
            </CardContent>
          </Card>
        </div>
      </Section>
      <Section title="Settings: a card for each knob">
        <Frame here="settings" app="console">
          <Settings />
        </Frame>
      </Section>
    </>
  );
}

const projects = [
  { name: "Hello World", slug: "hello-world", phase: "Running", web: "4/4", worker: "1/1", release: "v12", url: "hello-world.acme.shpyrd.app", created: "3 months ago" },
  { name: "Docs 001", slug: "docs001", phase: "Running", web: "2/2", release: "v4", url: "docs.acme.com", created: "2 months ago" },
  { name: "Billing", slug: "billing", phase: "Deploying", web: "1/2", worker: "2/2", release: "v31", url: "billing.acme.shpyrd.app", created: "5 weeks ago" },
  { name: "Reports", slug: "reports", phase: "Building", worker: "0/1", release: "v2", url: undefined, created: "8 days ago" },
  { name: "hello", slug: "hello", phase: "Failed", web: "0/1", release: "v1", url: "hello.acme.shpyrd.app", created: "2 days ago" },
] as const;

const phases = {
  Running: { type: "success", live: false },
  Deploying: { type: "warning", live: true },
  Building: { type: "info", live: true },
  Failed: { type: "error", live: false },
} as const;

function Phase({ phase }: { phase: keyof typeof phases }) {
  return (
    <StatusBadge type={phases[phase].type} live={phases[phase].live}>
      {phase}
    </StatusBadge>
  );
}

// `web 4/4`: how many instances of each process are ready.
function Processes({ web, worker }: { web?: string; worker?: string }) {
  const of = (qty: string) => {
    const [ready, wanted] = qty.split("/").map(Number);
    return ready === 0 ? "error" : ready < wanted ? "warning" : "success";
  };
  return (
    <Stack direction="horizontal" wrap="wrap" gap="tight">
      {web && (
        <StatusBadge variant="secondary" type={of(web)} qty={web} live={of(web) === "warning"}>
          web
        </StatusBadge>
      )}
      {worker && (
        <StatusBadge variant="secondary" type={of(worker)} qty={worker} live={of(worker) === "warning"}>
          worker
        </StatusBadge>
      )}
    </Stack>
  );
}

// The list: what there is, how each is, and the way to make one more.
function Projects() {
  return (
    <>
      <Alert variant="warning">
        <AlertTitle>Roles are not enforced yet</AlertTitle>
        <AlertDescription>
          Every signed-in person is an administrator until the first team exists. Make one with{" "}
          <InlineCode>shpyrd teams create platform</InlineCode> or on the Teams page.
        </AlertDescription>
      </Alert>
      <div className="grid gap-x-8 gap-y-4 @xl/page-layout:grid-cols-4">
        <Stat label="Projects" value={5} />
        <Stat label="Running" value={2} tone="success" />
        <Stat label="Building or deploying" value={2} tone="info" />
        <Stat label="Failed" value={1} tone="error" />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Projects</CardTitle>
          <CardDescription>
            Everything you deploy, from source to address. A project without a web process is a
            worker.
          </CardDescription>
          <CardAction>
            <NewProject />
          </CardAction>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Phase</TableHead>
                <TableHead>Processes</TableHead>
                <TableHead>Release</TableHead>
                <TableHead>Address</TableHead>
                <TableHead className="text-right">Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {projects.map((p) => (
                <TableRow key={p.slug}>
                  <TableCell>
                    <Stack direction="horizontal" align="center" gap="condensed">
                      <a href="#project" onClick={(e) => e.preventDefault()} className="font-medium hover:underline">
                        {p.name}
                      </a>
                      {p.name !== p.slug && <InlineCode>{p.slug}</InlineCode>}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Phase phase={p.phase} />
                  </TableCell>
                  <TableCell>
                    <Processes web={"web" in p ? p.web : undefined} worker={"worker" in p ? p.worker : undefined} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">{p.release}</TableCell>
                  <TableCell>
                    {p.url ? (
                      <a href="#open" onClick={(e) => e.preventDefault()} className="text-xs text-muted-foreground hover:underline">
                        {p.url}
                      </a>
                    ) : (
                      <span className="text-xs text-muted-foreground">none yet</span>
                    )}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">{p.created}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </>
  );
}

// The dialog that makes one: fields, a choice, and the two buttons.
function NewProject() {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button icon={<Plus />}>New project</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New project</DialogTitle>
          <DialogDescription>
            It answers at its address within a minute, with its certificate. The name may change
            later; the slug may not.
          </DialogDescription>
        </DialogHeader>
        <Stack gap="normal">
          <Field label="Name" required>
            <Input placeholder="Hello World" />
          </Field>
          <Field label="Slug" hint="Lowercase letters, digits and dashes." required>
            <Input placeholder="hello-world" suffix=".acme.shpyrd.app" />
          </Field>
          <Field label="Size">
            <Select defaultValue="shared-s">
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Pick a size" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="shared-s">
                  shared-s <span className="text-xs text-muted-foreground">0.5 CPU · 512 MiB</span>
                </SelectItem>
                <SelectItem value="shared-m">
                  shared-m <span className="text-xs text-muted-foreground">1 CPU · 1 GiB</span>
                </SelectItem>
                <SelectItem value="dedicated-l">
                  dedicated-l <span className="text-xs text-muted-foreground">2 CPU · 4 GiB</span>
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </Stack>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <DialogClose asChild>
            <Button onClick={() => toast.success("Project hello-world created", { description: "It answers at hello-world.acme.shpyrd.app." })}>
              Create
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const releases = [
  { number: 12, kind: "deploy", what: "Deploy of a3f9c21", build: "#48", created: "2 hours ago" },
  { number: 11, kind: "config", what: "DATABASE_URL set", build: "#47", created: "yesterday" },
  { number: 10, kind: "rollback", what: "Back to v8", build: "#45", created: "3 days ago" },
  { number: 9, kind: "deploy", what: "Deploy of 91be0d4", build: "#47", created: "4 days ago" },
];

// One thing: its heading with what can be done, what is going on, and its
// tabs.
function Project() {
  return (
    <>
      <PageHeading
        as="h2"
        title="Hello World"
        iconEnd={
          <>
            <InlineCode>hello-world</InlineCode>
            <Phase phase="Running" />
            <Badge variant="outline" className="gap-1">
              <Globe /> public
            </Badge>
            <Processes web="4/4" worker="1/1" />
          </>
        }
        description="hello-world.acme.shpyrd.app"
        actions={
          <>
            <Button variant="outline" size="sm" iconEnd={<ExternalLink />}>
              Open
            </Button>
            <DropdownButton
              size="sm"
              label="Deploy"
              onClick={() => toast("Deploying v13", { description: "Building a3f9c21 with buildpacks." })}
            >
              <DropdownMenuItem>
                <Upload /> Deploy from a folder
              </DropdownMenuItem>
              <DropdownMenuItem>
                <Rocket /> Redeploy the same build
              </DropdownMenuItem>
            </DropdownButton>
            <ConfirmDialog
              trigger={<Button variant="destructive" size="sm" icon={<Trash2 />} aria-label="Destroy" />}
              variant="destructive"
              title="Destroy the project?"
              description="Its instances stop and its address is released. This cannot be undone."
              confirmation="hello-world"
              action="Destroy the project"
              onConfirm={() => toast.error("hello-world destroyed")}
            />
          </>
        }
      />

      <Alert variant="info">
        <AlertTitle>Build #49 is running</AlertTitle>
        <AlertDescription>
          Cloning, then building with buildpacks. 1m 12s so far; the last one took 2m 40s.
        </AlertDescription>
        <AlertActions>
          <ProgressBar aria-label="Build" value={44} size="sm" className="w-48" />
          <Button size="xs" variant="outline">
            Follow the output
          </Button>
        </AlertActions>
      </Alert>

      <Tabs defaultValue="overview">
        <TabsList variant="line">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="metrics">Metrics</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="builds" counter={49}>
            Builds
          </TabsTrigger>
          <TabsTrigger value="config">Config</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="grid gap-6 pt-4">
          <div className="grid gap-4 @3xl/page-layout:grid-cols-3">
            <Card size="sm">
              <CardHeader>
                <CardTitle>Source</CardTitle>
              </CardHeader>
              <CardContent>
                <InfoTable layout="rows">
                  <InfoTableItem label="Git" mono truncate>
                    github.com/acme/hello-world
                  </InfoTableItem>
                  <InfoTableItem label="Revision" mono>
                    main
                  </InfoTableItem>
                  <InfoTableItem label="Build">Buildpacks</InfoTableItem>
                </InfoTable>
              </CardContent>
            </Card>
            <Card size="sm">
              <CardHeader>
                <CardTitle>Release</CardTitle>
              </CardHeader>
              <CardContent>
                <InfoTable layout="rows">
                  <InfoTableItem label="Current">v12 · Deploy of a3f9c21</InfoTableItem>
                  <InfoTableItem label="Build" mono>
                    #48
                  </InfoTableItem>
                  <InfoTableItem label="Hostname" mono truncate>
                    hello-world.acme.shpyrd.app
                  </InfoTableItem>
                </InfoTable>
              </CardContent>
            </Card>
            <Card size="sm">
              <CardHeader>
                <CardTitle>Processes</CardTitle>
                <CardAction>
                  <Button size="xs" variant="outline">
                    Apply
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent>
                <InfoTable layout="rows">
                  <InfoTableItem label="web">
                    <Select defaultValue="shared-s">
                      <SelectTrigger size="sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="shared-s">shared-s × 4</SelectItem>
                        <SelectItem value="shared-m">shared-m × 4</SelectItem>
                      </SelectContent>
                    </Select>
                  </InfoTableItem>
                  <InfoTableItem label="worker">
                    <Select defaultValue="shared-s">
                      <SelectTrigger size="sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="shared-s">shared-s × 1</SelectItem>
                        <SelectItem value="shared-m">shared-m × 1</SelectItem>
                      </SelectContent>
                    </Select>
                  </InfoTableItem>
                </InfoTable>
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Domains</CardTitle>
                <CardDescription>Addresses of your own that reach the project.</CardDescription>
              </CardHeader>
              <CardContent>
                <Blankslate
                  graphic={<Globe />}
                  title="No domain yet"
                  description="The project answers at its own address until one is added."
                  action={
                    <Button size="sm" variant="outline" icon={<Plus />}>
                      Add a domain
                    </Button>
                  }
                />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Activity</CardTitle>
                <CardDescription>What was done to the project, and by whom.</CardDescription>
              </CardHeader>
              <CardContent>
                <Timeline clip>
                  <TimelineItem condensed type="success" icon={<Rocket />}>
                    <b>Ana Souza</b> deployed v12 · 2 hours ago
                  </TimelineItem>
                  <TimelineItem condensed type="info" icon={<Settings2 />}>
                    <b>Marcelo Lima</b> set DATABASE_URL · yesterday
                  </TimelineItem>
                  <TimelineItem condensed type="warning" icon={<History />}>
                    <b>Ana Souza</b> rolled back to v8 · 3 days ago
                  </TimelineItem>
                </Timeline>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Releases</CardTitle>
              <CardDescription>
                A release is a build and its config. A rollback releases an earlier one again,
                exactly as it was.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Release</TableHead>
                    <TableHead>What changed</TableHead>
                    <TableHead>Build</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="text-right" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {releases.map((r, i) => (
                    <TableRow key={r.number}>
                      <TableCell className="font-mono text-xs">
                        v{r.number}{" "}
                        {i === 0 && (
                          <Badge variant="secondary" className="ml-1">
                            current
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" align="center" gap="condensed">
                          <Badge variant="outline">{r.kind}</Badge>
                          {r.what}
                        </Stack>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{r.build}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{r.created}</TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="outline"
                          size="xs"
                          disabled={i === 0}
                          title={i === 0 ? "The current release" : `Release v${r.number} again`}
                          onClick={() => toast.success(`Rolling back to v${r.number}`)}
                        >
                          Rollback
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="metrics" className="pt-4 text-sm text-muted-foreground">
          The <Link href="/layouts/metrics" className="text-primary underline-offset-4 hover:underline">Metrics of a project</Link> page shows this panel.
        </TabsContent>
        <TabsContent value="logs" className="pt-4">
          <Blankslate
            border
            title="The log view is not in the library yet"
            description="Lines as they come, with their level and their process, filtered. It is written in ui/src/components/log-view.tsx and has no component here."
          />
        </TabsContent>
        <TabsContent value="builds" className="pt-4 text-sm text-muted-foreground">
          The panel of builds: a table like the releases, with the output of each.
        </TabsContent>
        <TabsContent value="config" className="pt-4 text-sm text-muted-foreground">
          The panel of config: the variables, as fields.
        </TabsContent>
      </Tabs>
    </>
  );
}

// Settings: a heading, then a card for each knob, with what it is and
// what explains it.
function Settings() {
  return (
    <>
      <PageHeading
        as="h2"
        icon={<Settings2 />}
        title="Settings"
        description="The platform's own knobs. Sizes, global variables and drains are on the Cluster page."
      />
      <Card>
        <CardHeader>
          <CardTitle>Default workspace</CardTitle>
          <CardDescription>
            One of your workspaces: its owners are this console's platform admins, and commands
            over a kubeconfig act on its projects.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Select defaultValue="acme">
            <SelectTrigger className="w-72">
              <SelectValue placeholder="Choose a workspace" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="acme">
                Acme <span className="font-mono text-xs text-muted-foreground">acme.shpyrd.app</span>
              </SelectItem>
              <SelectItem value="ops">
                Operations <span className="font-mono text-xs text-muted-foreground">ops.shpyrd.app</span>
              </SelectItem>
            </SelectContent>
          </Select>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Email and password at the console</CardTitle>
          <CardDescription>
            On. Switch it off once your identity provider works, and the console has exactly the
            doors above.
          </CardDescription>
          <CardAction>
            <StatusBadge type="success">On</StatusBadge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <Button size="sm" variant="outline" onClick={() => toast("The console signs in through its identity providers only")}>
            Switch off
          </Button>
        </CardContent>
      </Card>
    </>
  );
}
