import { Cloud, KeyRound, PackageCheck } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact } from "@shpyrd/content/site/offer";
import { PartnerLimits } from "@/components/for-fde-partners";
import { pageSections } from "@/lib/page";

// For FDE partners - the overview: every client delivery on the same setup, a
// workspace per client on shpyrd cloud. The app is different for every client;
// what is around it (where it runs, who gets in, how it changes, the handover)
// is the part a partner stops redoing. Its subpages take one part each: the
// delivery, the handover, and the client's own cloud when they require it.

// TODO: the sign-up, once the cloud has one.
const signUp = "#";

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero variant="page" align="center"
        label="For FDE partners and teams that build for clients"
        heading="Make the next client delivery easier to repeat."
        description="The app is different for every client. Where it runs, who gets in and how it is handed over shouldn't be. On shpyrd cloud each client gets a workspace of its own, set up the same way every time."
        actions={
          <>
            <Button size="lg" asChild>
              <a href={signUp}>Get started</a>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <a href={contact.href}>Talk to us</a>
            </Button>
          </>
        }
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="What stays the same from one client to the next"
          description="The cluster, the certificates, the login, the access list, the handover: the work around the app is the part you stop doing by hand for each client."
        />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar variant="card"
            icon={<Cloud />}
            heading="A workspace per client"
            description="On shpyrd cloud, with nothing to install or run. When a client requires its own AWS or Oracle Cloud account, the same setup goes there."
          />
          <Pillar variant="card"
            icon={<KeyRound />}
            heading="The client's sign-in"
            description="Their Google Workspace, Microsoft Entra or Okta. Their groups become teams, so their people get in with the account they already have."
          />
          <Pillar variant="card"
            icon={<PackageCheck />}
            heading="A handover they can keep"
            description="Every change is a numbered release they can roll back. The platform is open source, so the client can read it."
          />
        </div>
      </Stack>


      <PartnerLimits />


    </PageLayoutContent>
  );
}
