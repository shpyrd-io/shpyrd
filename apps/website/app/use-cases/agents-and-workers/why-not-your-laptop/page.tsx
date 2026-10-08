import { Laptop } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { AddToAgent } from "@/components/add-to-agent";
import { NextSubpage } from "@/components/use-case-agents-and-workers";
import { BinaryMark } from "@/components/binary-mark";
import { CompareTable } from "@/components/compare-table";
import { pageSections } from "@/lib/page";

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
    <>
    {/* The section's background: the shpyrd mark made of falling binary
        (binary-mark.tsx), as on its overview. */}
    {/* The same size and place on every page of the section: 847px tall,
        its top 100px down, its centre 370px right of the page's middle. */}
    <BinaryMark height={847} offset={100} x={370} />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero align="center"
        variant="page"
        heading="Your agent shouldn't depend on your laptop being open."
        description="Line by line: what changes when it leaves the terminal tab, and the one thing that doesn't yet."
      />

      {/* Proposal (2026-10-08): the site's informative table (compare-table.tsx). */}
      <div data-binary-end className="mx-auto w-full">
        <CompareTable
          before="On your laptop"
          beforeIcon={<Laptop />}
          rows={rows.map((r) => ({
            label: r.q,
            before: r.today,
            pending: r.yet,
            after: r.yet ? (
              // On one line with the others: the badge before the words.
              <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
                <StatusBadge type="neutral">Not yet</StatusBadge>
                <span className="text-muted-foreground">Scheduled tasks are on the roadmap.</span>
              </span>
            ) : (
              r.shpyrd
            ),
          }))}
        />
      </div>

      <NextSubpage action={<AddToAgent />} href="/use-cases/agents-and-workers" title="Overview" />
    </PageLayoutContent>
    </>
  );
}
