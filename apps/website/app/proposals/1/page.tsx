import { ArrowDown, Globe, History, Plug, Rocket, Users } from "lucide-react";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Closing, ProposalNav, SignInScreen } from "@/components/proposals";

// Proposal 1 - the last step. Hypothesis H1: sharing is the unmet need. The
// reader is the builder, and the page starts where their app is today: working,
// on their laptop, for nobody else.

export const metadata = { title: "Proposal 1 · The last step" };

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <ProposalNav current={1} />

      <Hero
        label="For people who build with AI"
        heading="You built it. Now let your team use it."
        description="AI got your app working. shpyrd is the step after: its own address, a sign-in in front of it, and the colleagues you choose inside."
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
        image={
          <div className="grid gap-3">
            <BrowserFrame address="localhost:3000" secure={false} className="opacity-60">
              <p className="px-5 py-4 text-sm text-muted-foreground">
                Purchase requests · works for you, on your laptop
              </p>
            </BrowserFrame>
            <ArrowDown aria-hidden className="mx-auto size-5 text-muted-foreground" />
            <BrowserFrame address="purchases.acme.shpyrd.app">
              <SignInScreen app="Purchase requests" audience="Finance and Operations" />
            </BrowserFrame>
          </div>
        }
      />

      <Stack gap="spacious">
        <SectionIntro
          label="The part nobody plans for"
          heading="Working and used are two different things"
          description="Building the app was the part you knew how to ask for. The questions after it are the ones that keep it on your laptop."
        />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar
            icon={<Globe />}
            heading="Where does it live?"
            description="Not on your laptop, and not on a free host under your personal card. On shpyrd cloud, or on a cluster your company controls, at an address your colleagues can open."
          />
          <Pillar
            icon={<Users />}
            heading="Who can open it?"
            description="Finance and Operations, after signing in with their company account. Everyone else meets a sign-in page, not your app."
          />
          <Pillar
            icon={<History />}
            heading="What if I break it?"
            description="Every change is a numbered release. When an update breaks something people depend on, go back to the one before."
          />
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro
          heading="From your laptop to your team"
          description="The same agent that built the app takes it the rest of the way."
        />
        <Timeline clip className="max-w-prose">
          <TimelineItem icon={<Plug />}>
            <strong>Connect your agent to your workspace.</strong> One line in Claude Code,
            Codex or Cursor.
          </TimelineItem>
          <TimelineItem icon={<Rocket />}>
            <strong>Deploy the app.</strong> It gets an address, TLS, and a sign-in in front of
            it. Nobody gets in yet.
          </TimelineItem>
          <TimelineItem icon={<Users />} type="primary">
            <strong>Share it with the people it is for.</strong> A team, or named colleagues.
            They find it among their apps the next time they sign in.
          </TimelineItem>
          <TimelineItem icon={<History />}>
            <strong>Keep changing it.</strong> Each update is a release your colleagues get
            without asking; a bad one is undone in one step.
          </TimelineItem>
        </Timeline>
      </Stack>

      <Closing />
    </PageLayoutContent>
  );
}
