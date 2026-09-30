import Image from "next/image";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { boundariesSection, developerSection, hosting, pillars, situation, useCases } from "@shpyrd/content/site/home";
import { active } from "@shpyrd/content/site/messages";
import { offer, secondaryCta } from "@shpyrd/content/site/offer";
import shipyard from "@/images/shipyard.webp";
import { AddToAgent } from "@/components/add-to-agent";
import { Boundaries } from "@/components/boundaries";
import { Roster } from "@/components/roster";


export const metadata = {
  title: "shpyrd - One place to share apps with your team",
  description: active.explanation,
};

export default function Home() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        heading={active.headline}
        description={active.explanation}
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
        note={active.supporting}
        image={
          <Image
            src={shipyard}
            alt=""
            priority
            sizes="(min-width: 64rem) 32rem, 90vw"
            className="h-auto w-full"
          />
        }
      />

      <Stack gap="normal">
        <SectionIntro heading={situation.title} description={situation.body[0]} />
        <p className="max-w-prose">{situation.body[1]}</p>
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading={pillars.title} />
        {/* Three across, with the roster under them rather than beside: as a
            fourth column it left a hole the height of the section. */}
        <div className="grid gap-8 sm:grid-cols-3">
          {pillars.items.map((item) => (
            <Pillar key={item.id} heading={item.title} description={item.body} />
          ))}
        </div>
      </Stack>

      <SectionIntro
        heading={hosting.title}
        description={hosting.body[0]}
        link={
          <Button variant="link" asChild className="px-0">
            <a href={hosting.link.href}>{hosting.link.label}</a>
          </Button>
        }
      />

      <Stack gap="normal">
        <SectionIntro heading={useCases.title} description={useCases.disclaimer} />
        <Roster caption={useCases.caption} entries={useCases.apps} className="max-w-md" />
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading={offer.title} description={offer.intro} />
        <Button variant="outline" asChild className="self-start">
          <a href="/bring-an-app">Read what a session is</a>
        </Button>
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading={boundariesSection.title} description={boundariesSection.intro} />
        <Boundaries items={boundaries} />
      </Stack>

      <Stack gap="normal">
        <SectionIntro heading={developerSection.title} description={developerSection.body} />
        <pre className="max-w-prose overflow-x-auto rounded-lg border bg-muted p-4 text-sm">
          <code>{developerSection.code}</code>
        </pre>
        <Stack direction="horizontal" gap="cozy">
          {developerSection.links.map((link) => (
            <Button key={link.href} variant="outline" asChild>
              <a href={link.href}>{link.label}</a>
            </Button>
          ))}
        </Stack>
      </Stack>
    </PageLayoutContent>
  );
}
