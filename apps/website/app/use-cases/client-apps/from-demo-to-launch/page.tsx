import { Eye, Rocket, Undo2, Users } from "lucide-react";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { ClientAppsStep } from "@/components/use-case-client-apps";

// Client apps · Demo to day one: the life of one app inside the engagement,
// on one address, as a timeline. Each step is a grant or a release; nothing
// is rebuilt.

export const metadata = {
  title: "Client apps · From demo to launch",
  description:
    "One address for the client's app, from the first demo to the day their team starts using it, and every fix after.",
};

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="From the first demo to launch, on one address."
        description="The app you show the client on Tuesday is the one their team uses next month, on shpyrd cloud or in their own cloud. In between, only who can open it changes."
      />

      <Timeline clip className="max-w-prose">
        <TimelineItem icon={<Rocket />}>
          <strong>The first demo.</strong> You deploy it to the client&apos;s workspace. It has
          its address and sign-in from the start, and only your team can open it.
        </TimelineItem>
        <TimelineItem icon={<Eye />}>
          <strong>The client tries it.</strong> You add the three people who will sign it off, by
          email. They get in with their own work account.
        </TimelineItem>
        <TimelineItem icon={<Users />} type="primary">
          <strong>Go live.</strong> You give it to their Operations team. Everyone in that group
          can open it that morning, and nobody else.
        </TimelineItem>
        <TimelineItem icon={<Undo2 />}>
          <strong>The first fix.</strong> Every deploy is a numbered release. When one goes wrong,
          you put the previous one back, code and settings together, while you look.
        </TimelineItem>
      </Timeline>

      <ClientAppsStep next={{ href: "/use-cases/client-apps/the-delivery-sheet", label: "The delivery sheet" }} />
    </PageLayoutContent>
  );
}
