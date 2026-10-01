import { Building2, History, KeyRound, Moon, UserX, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { contact } from "@shpyrd/content/site/offer";
import { Boundaries } from "@/components/boundaries";
import { getStarted, ItNext } from "@/components/for-it";

// For IT teams, the section's own page. The apps people build with AI get one
// accepted place to run, on shpyrd cloud, behind your company's sign-in. The
// promise, what it does, the limits, and how to start. Its tabs go deeper:
// sign-in and access, the questions IT asks, and the checklist.

export const metadata = {
  title: "For IT teams",
  description:
    "Give the apps your people build one accepted place to run: on shpyrd cloud, behind your company's sign-in, with you deciding who uses and changes each one.",
};

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        label="For IT teams"
        heading="Give the apps your people build one place to run."
        description="Your colleagues are building tools with AI. shpyrd gives those apps one home on shpyrd cloud, behind your company's sign-in, with you deciding who can use and change each one. Nothing to install or operate."
        actions={
          <>
            <Button asChild>
              <a href={getStarted.href}>{getStarted.label}</a>
            </Button>
            <Button variant="outline" asChild>
              <a href={contact.href}>Talk to us about a pilot</a>
            </Button>
          </>
        }
        note="Policy says it must run in your own cloud? The same platform installs on AWS or Oracle Cloud."
      />

      <Stack gap="spacious">
        <SectionIntro heading="What you get" />
        <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-3">
          <Pillar
            icon={<Building2 />}
            heading="Your company's sign-in"
            description="Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider. Nobody gets another password."
          />
          <Pillar
            icon={<Users />}
            heading="Your groups become teams"
            description="A directory group maps to a team, so access follows the org chart you already keep."
          />
          <Pillar
            icon={<KeyRound />}
            heading="Using is not changing"
            description="Per app, some people use it, some change it, a few decide who's in. Most people only ever use."
          />
          <Pillar
            icon={<UserX />}
            heading="One switch per person"
            description="Suspend someone and every app closes to them at once, not app by app."
          />
          <Pillar
            icon={<History />}
            heading="Changes you can undo"
            description="Every deploy is a numbered release. A bad one is rolled back with its settings."
          />
          <Pillar
            icon={<Moon />}
            heading="Idle apps sleep"
            description="On shpyrd cloud an app nobody is using can sleep, and wakes on the next visit in a few seconds."
          />
        </div>
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading="What it doesn't do" description="Worth knowing before you approve anything." />
        <Boundaries items={boundaries.filter((b) => b.step !== null)} />
      </Stack>

      <ItNext />
    </PageLayoutContent>
  );
}
