import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { ClientAppsStep } from "@/components/use-case-client-apps";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { pageSections } from "@/lib/page";

// Client apps · The delivery sheet: the questions a client asks about every
// app it receives, answered as one short sheet for the delivery note, with the
// parts that stay the client's to do written as plainly as the rest.

export const metadata = {
  title: "Client apps · The delivery sheet",
  description:
    "What a client's app comes with when it runs on shpyrd: where it runs, who gets in, who can change it, and what is backed up.",
};

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero align="center"
        variant="page"
        heading="Send the app with answers, not a list of open questions."
        description="On shpyrd the answers are the same for every app you build, so you write them down once."
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="Northwind order portal"
          description="An example of the sheet. Only the names change from one client to the next."
        />
        <Card className={cn(glass, "p-6")}>
          <InfoTable columns={2}>
            <InfoTableItem label="Runs on">A Northwind workspace on shpyrd cloud (or in Northwind&apos;s own cloud)</InfoTableItem>
            <InfoTableItem label="Address" mono>
              orders.northwind.shpyrd.app
            </InfoTableItem>
            <InfoTableItem label="Sign-in">Northwind&apos;s Microsoft Entra</InfoTableItem>
            <InfoTableItem label="Can use it">The Operations team, from Northwind&apos;s directory</InfoTableItem>
            <InfoTableItem label="Can change it">Our engineers, until the handover</InfoTableItem>
            <InfoTableItem label="Decides who's in">Northwind&apos;s IT</InfoTableItem>
            <InfoTableItem label="Changes">Numbered releases; any one can be put back</InfoTableItem>
            <InfoTableItem label="Source of the platform">Open source, MPL-2.0</InfoTableItem>
            <InfoTableItem label="Backed up nightly" span="full">
              The workspace itself: projects, settings, teams and who can open what. Once a
              backup bucket is set up.
            </InfoTableItem>
            <InfoTableItem label="Yours to back up" span="full">
              The data inside the app&apos;s database and volumes. The platform backup
              recreates them empty.
            </InfoTableItem>
          </InfoTable>
        </Card>
      </Stack>

      <ClientAppsStep next={{ href: "/use-cases/client-apps/tell-your-agent", label: "Tell your agent" }} />
    </PageLayoutContent>
  );
}
