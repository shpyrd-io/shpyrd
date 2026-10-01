import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { AddToAgent } from "@/components/add-to-agent";
import { NextSubpage } from "@/components/use-case-agents-and-workers";

// Agents and workers · Laptop vs shpyrd. One job: the setup it runs on today,
// answered line by line, including the one thing shpyrd doesn't do yet (a
// schedule). The last subpage: its ending goes back to the section's page.

export const metadata = { title: "Agents and workers · Why not your laptop" };

const rows: { q: string; today: string; shpyrd: string; yet?: boolean }[] = [
  { q: "Your laptop closes", today: "The agent stops.", shpyrd: "Nothing stops. It runs on shpyrd cloud, not on you." },
  { q: "It crashes at 3am", today: "It stays down until you notice.", shpyrd: "It starts again on its own." },
  { q: "Its API keys", today: "In a .env file, on your disk.", shpyrd: "Config vars: set once, never shown again." },
  { q: "What did it do?", today: "Whatever scrolled past in your terminal.", shpyrd: "Its log, live, named worker.1, or sent to your log provider." },
  { q: "A change breaks it", today: "Undo by hand and hope.", shpyrd: "Roll back to the previous release." },
  { q: "Run it every morning at 8", today: "A cron on your laptop.", shpyrd: "Not yet. Scheduled tasks are on the roadmap.", yet: true },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="Your agent shouldn't depend on your laptop being open."
        description="Line by line: what changes when it leaves the terminal tab, and the one thing that doesn't yet."
      />

      <Card className="overflow-x-auto p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-1/5" />
              <TableHead className="w-2/5">On your laptop</TableHead>
              <TableHead className="w-2/5">On shpyrd</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.q}>
                <TableCell className="align-top font-medium whitespace-normal">{r.q}</TableCell>
                <TableCell className="align-top whitespace-normal text-muted-foreground">{r.today}</TableCell>
                <TableCell className="align-top whitespace-normal">
                  {r.yet ? (
                    <span className="grid justify-items-start gap-1.5">
                      <StatusBadge type="neutral">Not yet</StatusBadge>
                      <span className="text-muted-foreground">Scheduled tasks are on the roadmap.</span>
                    </span>
                  ) : (
                    r.shpyrd
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <NextSubpage action={<AddToAgent />} href="/use-cases/agents-and-workers" title="Overview" />
    </PageLayoutContent>
  );
}
