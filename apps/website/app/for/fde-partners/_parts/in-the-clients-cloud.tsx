import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { infoTable } from "@/lib/info-table";
import { pageSections } from "@/lib/page";

// For FDE partners - in the client's cloud: the option, last, for a client
// that requires its own account. The same setup in their AWS (EKS) or Oracle
// Cloud (OKE), from the reference Terraform and one command (docs: aws,
// oracle-cloud, backups), and what that asks of them.

const asks = [
  { what: "An account", how: "Their own AWS or Oracle Cloud account, where the cluster and its network are created." },
  { what: "A setup", how: "The reference Terraform for the network and the cluster, then shpyrd cluster init with the cloud's profile." },
  { what: "Someone to run it", how: "The cluster is theirs to keep up: you, or the client's own team. It isn't a managed service." },
  { what: "A bucket for backups", how: "Nightly encrypted backups of the platform go to their own object storage, outside the cluster. The data inside the apps' databases needs its own." },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero as="h2" align="center"
        variant="page"
        heading="When the client needs it in their own cloud."
        description="Most deliveries start on shpyrd cloud. When a client requires its own account, the same workspace, sign-in and releases run in their AWS or Oracle Cloud."
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="What that asks of the client" />
        <Card className={cn(glass, infoTable, "overflow-x-auto p-0")}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-1/3" />
                <TableHead>What it means</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {asks.map((a) => (
                <TableRow key={a.what}>
                  <TableCell className="align-top font-medium whitespace-normal">{a.what}</TableCell>
                  <TableCell className="align-top whitespace-normal text-muted-foreground">{a.how}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </Stack>

    </PageLayoutContent>
  );
}
