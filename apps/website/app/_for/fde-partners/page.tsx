import { Cloud, KeyRound, Package, PackageCheck, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { contact } from "@shpyrd/content/site/offer";
import { PartnerLimits, PartnerNext } from "@/components/for-fde-partners";

// For FDE partners - the overview: every client delivery on the same setup, a
// workspace per client on shpyrd cloud. The app is different for every client;
// what is around it (where it runs, who gets in, how it changes, the handover)
// is the part a partner stops redoing. Its subpages take one part each: the
// delivery, the handover, and the client's own cloud when they require it.

export const metadata = { title: "For FDE partners" };

// TODO: the sign-up, once the cloud has one.
const signUp = "#";

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        label="For FDE partners and teams that build for clients"
        heading="Make the next client delivery easier to repeat."
        description="The app is different for every client. Where it runs, who gets in and how it is handed over shouldn't be. On shpyrd cloud each client gets a workspace of its own, set up the same way every time."
        actions={
          <>
            <Button asChild>
              <a href={signUp}>Get started</a>
            </Button>
            <Button variant="outline" asChild>
              <a href={contact.href}>Talk to us</a>
            </Button>
          </>
        }
      />

      <Stack gap="spacious">
        <SectionIntro
          heading="What stays the same from one client to the next"
          description="The cluster, the certificates, the login, the access list, the handover: the work around the app is the part you stop doing by hand for each client."
        />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar
            icon={<Cloud />}
            heading="A workspace per client"
            description="On shpyrd cloud, with nothing to install or run. When a client requires its own AWS or Oracle Cloud account, the same setup goes there."
          />
          <Pillar
            icon={<KeyRound />}
            heading="The client's sign-in"
            description="Their Google Workspace, Microsoft Entra or Okta. Their groups become teams, so their people get in with the account they already have."
          />
          <Pillar
            icon={<PackageCheck />}
            heading="A handover they can keep"
            description="Every change is a numbered release they can roll back. The platform is open source, so the client can read it."
          />
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro
          heading="One delivery, the same four steps every time"
          description="The first client teaches you the setup. The second is where it pays."
        />
        <Timeline clip className="max-w-prose">
          <TimelineItem icon={<Cloud />}>
            <strong>Open the client&apos;s workspace.</strong> On shpyrd cloud, at an address of its
            own, and on the client&apos;s domain when they bring one.
          </TimelineItem>
          <TimelineItem icon={<Rocket />}>
            <strong>Deploy what you built.</strong> From source or a Dockerfile. It gets an address and
            a certificate, and a sign-in in front of it.
          </TimelineItem>
          <TimelineItem icon={<Users />} type="primary">
            <strong>Let the client&apos;s people in.</strong> Their teams can use it; your engineers
            can change it; their IT decides who is in.
          </TimelineItem>
          <TimelineItem icon={<Package />}>
            <strong>Hand it over.</strong> Take your team off when the work ends. The workspace, its
            releases and its people stay with the client.
          </TimelineItem>
        </Timeline>
      </Stack>

      <PartnerLimits />

      <PartnerNext />
    </PageLayoutContent>
  );
}
