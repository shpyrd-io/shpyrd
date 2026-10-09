import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { infoTable } from "@/lib/info-table";
import { pageSections } from "@/lib/page";

// Apps from a hackathon - the rollout checklist. The research's content idea
// ("a rollout checklist for the three useful apps from your AI hackathon"):
// six checks, and who covers each.

type Row = { check: string; how: string; who: "shpyrd" | "you" };

const rows: Row[] = [
  { check: "It runs somewhere other than a laptop", how: "One sentence to your agent puts it at its own address, with a certificate.", who: "shpyrd" },
  { check: "People sign in with their work account", how: "The sign-in sits in front of the app. The app needs no login of its own.", who: "shpyrd" },
  { check: "Only the right team can open it", how: "Share it with Finance, or with named people. Everyone else is turned away.", who: "shpyrd" },
  { check: "Someone other than the builder can change it", how: "Give a second person the right to update it, separately from using it.", who: "shpyrd" },
  { check: "A bad change can be undone", how: "Every change is a numbered release. Go back to the one before in one step.", who: "shpyrd" },
  { check: "Someone owns it after the hackathon", how: "A name next to the app, and time in their week for it. No platform does this for you.", who: "you" },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2" align="center"
        variant="page"
        heading="Six checks before a hackathon app goes to work."
        description="Five of them are one sentence to your agent. The sixth is a person."
      />

      <Card className={cn(glass, infoTable, "overflow-x-auto p-0")}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-2/5">Check</TableHead>
              <TableHead>How</TableHead>
              <TableHead className="text-right">Who</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.check}>
                <TableCell className="align-top font-medium whitespace-normal">{r.check}</TableCell>
                <TableCell className="align-top whitespace-normal text-muted-foreground">{r.how}</TableCell>
                <TableCell className="text-right align-top">
                  <StatusBadge type={r.who === "shpyrd" ? "success" : "neutral"}>
                    {r.who === "shpyrd" ? "shpyrd" : "You"}
                  </StatusBadge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

    </PageLayoutContent>
  );
}
