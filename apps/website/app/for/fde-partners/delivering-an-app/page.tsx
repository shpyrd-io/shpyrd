import { Eye, Hammer, KeyRound, Rocket, Undo2, Users } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { PartnerStep } from "@/components/for-fde-partners";
import { VerticalRoute } from "@/components/vertical-route";
import { pageSections } from "@/lib/page";

// For FDE partners - delivering an app: who does what on one client app (the
// partner's engineers, the client's IT, the client's people, docs/access), and
// how access changes from the first demo to go-live on the same address. The
// end of the work is the next tab.

export const metadata = { title: "For FDE partners · Delivering an app" };

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero align="center"
        variant="page"
        heading="Your engineers ship it. Their people use it. Their IT decides who's in."
        description="Three kinds of people touch every client app. In the client's workspace each has exactly their part, so nobody shares an admin password to get work done."
      />

      <div className="grid gap-8 sm:grid-cols-3">
        <Pillar variant="card"
          icon={<Hammer />}
          heading="Your engineers"
          description="Deploy, roll back, change settings, read logs and metrics. Everything it takes to deliver, and nothing that decides who at the client gets in."
        />
        <Pillar variant="card"
          icon={<KeyRound />}
          heading="The client's IT"
          description="Connects their company sign-in, maps their groups to teams, and chooses who can use each app."
        />
        <Pillar variant="card"
          icon={<Eye />}
          heading="The client's people"
          description="Sign in with the account they already have and use the app. They see their apps and nothing of how they run."
        />
      </div>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="From the first demo to go-live, on one address"
          description="The app never moves. Only who can open it changes."
        />
        {/* The steps as the home page's route, standing up (vertical-route.tsx). */}
        <VerticalRoute
          className="mx-auto w-full max-w-2xl"
          steps={[
            {
              icon: <Rocket />,
              children: (
                <p>
                  <strong>First demo.</strong> Only your team can open it; anyone else meets a sign-in page.
                </p>
              ),
            },
            {
              icon: <Eye />,
              children: (
                <p>
                  <strong>The client tries it.</strong> A few of their people, by email, can use it.
                </p>
              ),
            },
            {
              icon: <Users />,
              lit: true,
              children: (
                <p>
                  <strong>Go-live.</strong> Their Operations group, from their directory, becomes the team
                  that uses it.
                </p>
              ),
            },
            {
              icon: <Undo2 />,
              children: (
                <p>
                  <strong>The first fix.</strong> A release that breaks something is rolled back in one
                  step, while you fix it.
                </p>
              ),
            },
          ]}
        />
      </Stack>

      <PartnerStep next={{ href: "/for/fde-partners/handing-it-over", title: "Handing it over" }} />
    </PageLayoutContent>
  );
}
