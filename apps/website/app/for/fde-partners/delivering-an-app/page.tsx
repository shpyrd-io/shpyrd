import { Eye, Hammer, KeyRound } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { PartnerStep } from "@/components/for-fde-partners";

// For FDE partners - delivering an app: who does what on one client app (the
// partner's engineers, the client's IT, the client's people, docs/access), and
// how access changes from the first demo to go-live on the same address. The
// end of the work is the next tab.

export const metadata = { title: "For FDE partners · Delivering an app" };

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        variant="medium"
        heading="Your engineers ship it. Their people use it. Their IT decides who's in."
        description="Three kinds of people touch every client app. In the client's workspace each has exactly their part, so nobody shares an admin password to get work done."
      />

      <div className="grid gap-8 sm:grid-cols-3">
        <Pillar
          icon={<Hammer />}
          heading="Your engineers"
          description="Deploy, roll back, change settings, read logs and metrics. Everything it takes to deliver, and nothing that decides who at the client gets in."
        />
        <Pillar
          icon={<KeyRound />}
          heading="The client's IT"
          description="Connects their company sign-in, maps their groups to teams, and chooses who can use each app."
        />
        <Pillar
          icon={<Eye />}
          heading="The client's people"
          description="Sign in with the account they already have and use the app. They see their apps and nothing of how they run."
        />
      </div>

      <Stack gap="spacious">
        <SectionIntro
          heading="From the first demo to go-live, on one address"
          description="The app never moves. Only who can open it changes."
        />
        <Timeline clip className="max-w-prose">
          <TimelineItem>
            <strong>First demo.</strong> Only your team can open it; anyone else meets a sign-in page.
          </TimelineItem>
          <TimelineItem>
            <strong>The client tries it.</strong> A few of their people, by email, can use it.
          </TimelineItem>
          <TimelineItem type="primary">
            <strong>Go-live.</strong> Their Operations group, from their directory, becomes the team
            that uses it.
          </TimelineItem>
          <TimelineItem>
            <strong>The first fix.</strong> A release that breaks something is rolled back in one
            step, while you fix it.
          </TimelineItem>
        </Timeline>
      </Stack>

      <PartnerStep next={{ href: "/for/fde-partners/handing-it-over", title: "Handing it over" }} />
    </PageLayoutContent>
  );
}
