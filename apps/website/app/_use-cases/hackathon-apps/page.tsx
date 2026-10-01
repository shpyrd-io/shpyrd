import { Filter, MessageSquare, LayoutGrid } from "lucide-react";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { AddToAgent } from "@/components/add-to-agent";
import { Launcher, launcherApps } from "@/components/proposals";
import { HackathonNext } from "@/components/use-case-hackathon-apps";

// Apps from a hackathon - the section's own page: keep the few. The research's
// point: a hackathon's count is activity, not value; the value is the handful
// of apps people want next week. Three steps, the launcher as the proof, and
// the full ending with the shipyard. Its subpages each do one thing more.

export const metadata = {
  title: "Apps from a hackathon",
  description:
    "Your hackathon made dozens of apps. Keep the few people want, and put them in front of their colleagues next week.",
};

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        label="Apps from a hackathon"
        heading="The hackathon made dozens of apps. Keep the ones people want."
        description="Most of them were for the demo. A few are the ones colleagues keep asking about. Put those in front of the people who need them, next week, not next quarter."
        actions={<AddToAgent />}
        image={
          <BrowserFrame address="acme.shpyrd.app">
            <Launcher person="luis@acme.com" apps={launcherApps.slice(0, 4)} />
          </BrowserFrame>
        }
      />

      <Stack gap="spacious">
        <SectionIntro heading="From demo day to the working week" />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar
            icon={<Filter />}
            heading="Pick the few"
            description="The apps someone asked for after the demo. Not all of them: the ones a team would miss."
          />
          <Pillar
            icon={<MessageSquare />}
            heading="Tell your agent who each is for"
            description="“Share the expense splitter with Finance.” It gets an address on shpyrd cloud, a sign-in, and the people you named."
          />
          <Pillar
            icon={<LayoutGrid />}
            heading="They find it on Monday"
            description="Colleagues sign in with their work account and see the apps shared with them, nothing else."
          />
        </div>
      </Stack>

      <HackathonNext />
    </PageLayoutContent>
  );
}
