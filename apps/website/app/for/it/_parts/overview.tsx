import { History, KeyRound, Moon } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { contact } from "@shpyrd/content/site/offer";
import { Boundaries } from "@/components/boundaries";
import { getStarted } from "@/components/for-it";
import { pageSections } from "@/lib/page";

// For IT teams, the section's own page. The apps people build with AI get one
// accepted place to run, on shpyrd cloud, behind your company's sign-in. The
// promise, what it does, the limits, and how to start. Its tabs go deeper:
// sign-in and access, the questions IT asks, and the checklist.

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero variant="page" align="center"
        label="For IT teams"
        heading="Give the apps your people build one place to run."
        description="Your colleagues are building tools with AI. shpyrd gives those apps one home on shpyrd cloud, behind your company's sign-in, with you deciding who can use and change each one. Nothing to install or operate."
        actions={
          <>
            <Button size="lg" asChild>
              <a href={getStarted.href}>{getStarted.label}</a>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <a href={contact.href}>Talk to us about a pilot</a>
            </Button>
          </>
        }
        note="Policy says it must run in your own cloud? The same platform installs on AWS or Oracle Cloud."
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="What you get" />
        <div className="grid gap-8 sm:grid-cols-3">
          <Pillar variant="card"
            icon={<KeyRound />}
            heading="Using is not changing"
            description="Per app, some people use it, some change it, a few decide who's in. Most people only ever use."
          />
          <Pillar variant="card"
            icon={<History />}
            heading="Changes you can undo"
            description="Every deploy is a numbered release. A bad one is rolled back with its settings."
          />
          <Pillar variant="card"
            icon={<Moon />}
            heading="Idle apps sleep"
            description="On shpyrd cloud an app nobody is using can sleep, and wakes on the next visit in a few seconds."
          />
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="What it doesn't do" description="Worth knowing before you approve anything." />
        <Boundaries items={boundaries.filter((b) => b.step !== null)} />
      </Stack>


    </PageLayoutContent>
  );
}
