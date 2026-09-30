import { Eye, Hammer, History, KeyRound, Rocket, ShieldCheck } from "lucide-react";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Closing, Launcher, launcherApps, ProposalNav } from "@/components/proposals";

// Proposal 3 - the workspace. Hypotheses H2 (a shared destination is the
// value) and H5 (use, update and manage are different jobs). The page shows
// the product before it explains it, from the side of each person it serves.

export const metadata = { title: "Proposal 3 · The workspace" };

const access = [
  { app: "Purchase requests", use: "Finance, Operations", update: "Marta Silva" },
  { app: "Onboarding checklist", use: "People, managers", update: "João Reis" },
  { app: "Quote tool", use: "Sales", update: "Sales ops" },
  { app: "Field reports", use: "Nobody yet", update: "Rui Costa" },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <ProposalNav current={3} />

      <Hero
        heading="Your team's apps, in one place."
        description="The tools your people build with AI, where their colleagues can find them. Choose who can use each app, who can change it, and who looks after the lot."
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
        note="shpyrd runs the apps and decides who gets in. Your team builds them, with the tools it already uses."
        image={
          <BrowserFrame address="acme.shpyrd.app">
            <Launcher person="ana@acme.com" apps={launcherApps.slice(0, 4)} />
          </BrowserFrame>
        }
      />

      <Stack gap="spacious">
        <SectionIntro
          heading="One workspace, seen three ways"
          description="The same apps, and what each person can do with them."
        />
        <Tabs defaultValue="use" className="min-w-0">
          <TabsList className="max-w-full overflow-x-auto overflow-y-hidden">
            <TabsTrigger value="use" icon={<Eye />}>Someone in Finance</TabsTrigger>
            <TabsTrigger value="update" icon={<Hammer />}>Whoever built it</TabsTrigger>
            <TabsTrigger value="manage" icon={<KeyRound />}>Whoever runs it</TabsTrigger>
          </TabsList>

          <TabsContent value="use" className="grid gap-6 pt-6 md:grid-cols-2 md:items-start">
            <BrowserFrame address="acme.shpyrd.app">
              <Launcher
                person="luis@acme.com"
                apps={launcherApps.filter((a) => ["Purchase requests", "Weekly numbers"].includes(a.name))}
              />
            </BrowserFrame>
            <Stack gap="condensed">
              <p className="font-heading text-lg font-medium">They see the apps shared with them. Nothing else.</p>
              <p className="text-muted-foreground">
                Luís signs in with his company account and finds the two apps Finance was given.
                He can use them. He cannot change them, and he never sees the ones that were not
                shared with him.
              </p>
            </Stack>
          </TabsContent>

          <TabsContent value="update" className="grid gap-6 pt-6 md:grid-cols-2 md:items-start">
            <Timeline clip className="rounded-xl border px-4">
              <TimelineItem icon={<History />} type="primary">
                <strong>Release 3</strong> rolled back to release 2 · 09:40
              </TimelineItem>
              <TimelineItem icon={<Rocket />}>
                <strong>Release 3</strong> deployed by Marta · 09:12
              </TimelineItem>
              <TimelineItem icon={<Rocket />}>
                <strong>Release 2</strong> added the Finance export · yesterday
              </TimelineItem>
              <TimelineItem icon={<Rocket />}>
                <strong>Release 1</strong> first deploy · Monday
              </TimelineItem>
            </Timeline>
            <Stack gap="condensed">
              <p className="font-heading text-lg font-medium">They can ship changes, and take them back.</p>
              <p className="text-muted-foreground">
                Marta built the purchase tracker and can update it. Every change is a numbered
                release; when one breaks the totals, she goes back to the one before, and so can
                anyone else with update rights while she is away.
              </p>
            </Stack>
          </TabsContent>

          <TabsContent value="manage" className="grid gap-6 pt-6">
            <div className="overflow-x-auto rounded-xl border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>App</TableHead>
                    <TableHead>Can use</TableHead>
                    <TableHead>Can update</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {access.map((row) => (
                    <TableRow key={row.app}>
                      <TableCell className="font-medium">{row.app}</TableCell>
                      <TableCell>
                        {row.use === "Nobody yet" ? (
                          <StatusBadge type="neutral">Not shared yet</StatusBadge>
                        ) : (
                          row.use
                        )}
                      </TableCell>
                      <TableCell>{row.update}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <p className="max-w-prose text-muted-foreground">
              The admin sees every app and who is in each. Access follows teams: whoever is
              added to Finance gets Finance&apos;s apps, without anyone going app by app.
            </p>
          </TabsContent>
        </Tabs>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro
          heading="What it is, and what it isn't"
          description="Worth saying before anyone expects the wrong thing."
        />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar
            icon={<ShieldCheck />}
            heading="A gate, not a rulebook"
            description="shpyrd decides who can open an app. Who can approve an expense inside it is still the app's job."
          />
          <Pillar
            icon={<Eye />}
            heading="Your apps, not all apps"
            description="The workspace holds what you ship through shpyrd. It doesn't find or manage the SaaS your company already uses."
          />
          <Pillar
            icon={<Hammer />}
            heading="It runs apps; it doesn't write them"
            description="Your team builds with Claude Code, Codex or whatever it uses today. shpyrd is where the result goes."
          />
        </div>
      </Stack>

      <Closing />
    </PageLayoutContent>
  );
}
