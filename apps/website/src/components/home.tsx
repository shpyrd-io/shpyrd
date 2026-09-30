import Link from "next/link";
import { Globe, History, Plug, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { also, gap, hero, route } from "@shpyrd/content/site/home";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Closing } from "@/components/proposals";

// The homepage, for the person who built an app with AI and cannot yet put it in
// front of anyone (homepage proposal 1, chosen 2026-09-30). It starts where
// their app is today: working, on their laptop, for nobody else. The other
// people who choose shpyrd have a page each, reached from "Also for" and the
// Solutions menu.
//
// The picture beside the heading is a part the page is given: "/" gives the
// chat, and a proposal can give another.

// The icons of the page, by the id of what they stand beside.
const gapIcons: Record<string, React.ReactElement> = {
  where: <Globe />,
  who: <Users />,
  break: <History />,
};
const stepIcons: Record<string, React.ReactElement> = {
  connect: <Plug />,
  deploy: <Rocket />,
  share: <Users />,
  change: <History />,
};

export function Home({ picture, before }: { picture: React.ReactNode; before?: React.ReactNode }) {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      {before}
      <Hero
        heading={hero.heading}
        description={hero.description}
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
        image={picture}
      />

      <Stack gap="spacious">
        <SectionIntro label={gap.label} heading={gap.heading} description={gap.description} />
        <div className="grid gap-8 sm:grid-cols-3">
          {gap.items.map((item) => (
            <Pillar key={item.id} icon={gapIcons[item.id]} heading={item.title} description={item.body} />
          ))}
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro heading={route.heading} description={route.description} />
        <Timeline clip className="max-w-prose">
          {route.steps.map((step) => (
            <TimelineItem
              key={step.id}
              icon={stepIcons[step.id]}
              type={step.id === "share" ? "primary" : undefined}
            >
              <strong>{step.title}</strong> {step.body}
            </TimelineItem>
          ))}
        </Timeline>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro label={also.label} heading={also.heading} />
        <ul className="grid gap-4 sm:grid-cols-3">
          {also.items.map((a) => (
            <li key={a.href}>
              <Card asChild className="h-full">
                <Link href={a.href}>
                  <CardHeader>
                    <CardTitle className="font-heading">{a.title} →</CardTitle>
                    <CardDescription>{a.body}</CardDescription>
                  </CardHeader>
                </Link>
              </Card>
            </li>
          ))}
        </ul>
      </Stack>

      <Closing />
    </PageLayoutContent>
  );
}
