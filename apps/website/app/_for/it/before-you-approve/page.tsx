import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { ItNextStep } from "@/components/for-it";

// For IT teams · before you approve. What IT checks before an app runs for
// the company, on shpyrd cloud, each line marked honestly: shpyrd does it, it
// is yours to do, or it is not there yet. The "not yet" rows are the point.

export const metadata = { title: "For IT teams · Before you approve" };

type Status = "ships" | "yours" | "later";

const badge: Record<Status, { type: "success" | "neutral" | "warning"; label: string }> = {
  ships: { type: "success", label: "shpyrd does it" },
  yours: { type: "neutral", label: "Yours to do" },
  later: { type: "warning", label: "Not yet" },
};

const rows: { need: string; how: string; status: Status }[] = [
  { need: "People sign in with the company account", how: "Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider, per workspace.", status: "ships" },
  { need: "Access follows the directory", how: "Identity-provider groups map to teams; grants go to teams.", status: "ships" },
  { need: "Only the right people reach an app", how: "Checked before the request reaches the app; others see which team it is for.", status: "ships" },
  { need: "Using and changing are separate", how: "Per app: reader, user, viewer, developer, admin.", status: "ships" },
  { need: "Someone leaves", how: "Suspend them and every app closes to them at once.", status: "ships" },
  { need: "A bad update can be undone", how: "Numbered releases; rollback restores code and settings.", status: "ships" },
  { need: "Nothing to install or operate", how: "A workspace on shpyrd cloud, with its own address; your own domains too.", status: "ships" },
  { need: "Must run in our own cloud", how: "Possible: the same platform on AWS (EKS) or Oracle Cloud (OKE), where apps can also be internal-only. Someone then owns that cluster and its upgrades.", status: "yours" },
  { need: "Permissions inside the app", how: "The app is told who is there and their teams; finer rules are the app's code.", status: "yours" },
  { need: "The workspace is backed up", how: "Nightly and encrypted: projects, settings, teams and who can open what.", status: "ships" },
  { need: "The data is backed up", how: "Switch on a database's continuous backups; files on volumes need their own.", status: "yours" },
  { need: "A long-term audit record", how: "Platform changes are recorded, but kept for about an hour today.", status: "later" },
  { need: "Password reset and account verification", how: "For people using your company's sign-in, your provider does this; for local accounts it is on the roadmap.", status: "later" },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="Before you approve an app, check this list."
        description="What IT asks of an internal app on shpyrd cloud, and how each is answered: done for you, yours to do, or not there yet."
      />

      <Card className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-1/3">You need</TableHead>
                <TableHead>How</TableHead>
                <TableHead className="text-right">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.need}>
                  <TableCell className="align-top font-medium whitespace-normal">{row.need}</TableCell>
                  <TableCell className="align-top whitespace-normal text-muted-foreground">{row.how}</TableCell>
                  <TableCell className="text-right align-top">
                    <StatusBadge type={badge[row.status].type}>{badge[row.status].label}</StatusBadge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>

      <ItNextStep next={{ label: "Overview", href: "/for/it" }} />
    </PageLayoutContent>
  );
}
