import { Building2, KeyRound, Users } from "lucide-react";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { Boundaries } from "@/components/boundaries";
import { SignInScreen } from "@/components/proposals";
import { pageSections } from "@/lib/page";

// Client apps, the section's own page: built by you, belonging to them. What a
// client accepts first is how its own people get in: the account they already
// have, and the groups their IT already keeps. The page starts at the client's
// sign-in page; the subpages follow one app through a delivery.

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero variant="page"
        label="Client apps"
        heading="Their people open it with the account they already have."
        description="You built the app for a client. It runs in a workspace on shpyrd cloud or in their own cloud, behind their company's sign-in, and their own groups decide who gets in. Nobody gets a new password to remember."
        image={
          <BrowserFrame address="orders.northwind.shpyrd.app">
            <SignInScreen app="Order portal" audience="Northwind's Operations team" />
          </BrowserFrame>
        }
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="Built by you, belonging to them" />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar variant="card"
            icon={<Building2 />}
            heading="shpyrd cloud, or theirs"
            description="A workspace on shpyrd cloud, with nothing for anyone to run. Or a cluster in the client's own AWS or Oracle Cloud account, if they want it there."
          />
          <Pillar variant="card"
            icon={<KeyRound />}
            heading="Their sign-in"
            description="Microsoft Entra, Google Workspace, Okta or any OpenID provider the client already uses. Their people sign in the way they do everywhere else."
          />
          <Pillar variant="card"
            icon={<Users />}
            heading="Their groups"
            description="A group in their directory becomes a team with the app. When their IT adds someone to Operations, that person can open it."
          />
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="Worth knowing before you promise it" />
        <Boundaries items={boundaries.filter((b) => ["sign-in", "data"].includes(b.id))} />
      </Stack>


    </PageLayoutContent>
  );
}
