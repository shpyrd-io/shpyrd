import Link from "next/link";
import { History, Plug, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { also, gap, hero, route } from "@shpyrd/content/site/home";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { BinaryOcean } from "@/components/binary-ocean";
import { FeatureBlocks } from "@/components/feature-blocks";
import { RouteTimeline } from "@/components/route-timeline";
import { cn } from "@shpyrd/ui/lib/cn";
import { sectionsLive } from "@/lib/sections";
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
const stepIcons: Record<string, React.ReactElement> = {
  connect: <Plug />,
  deploy: <Rocket />,
  share: <Users />,
  change: <History />,
};

export function Home({ picture, before }: { picture: React.ReactNode; before?: React.ReactNode }) {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className="grid content-start gap-16 py-8">
      {before}
      {/* The hero sits on the binary ocean of the launch film, which runs
          from edge to edge behind it, faded at both ends. */}
      {/* The hero reaches to 76% of the window on a wide screen, its words in
          the middle of it, so a strip of what follows still shows under it. */}
      <div className="relative isolate md:grid md:min-h-[calc(76svh-6rem)] md:content-center">
        <BinaryOcean className="pointer-events-none absolute -top-6 left-1/2 -z-10 h-[calc(100%+4rem)] w-screen -translate-x-1/2 [mask-image:linear-gradient(to_bottom,transparent,black_15%,black_70%,transparent)]" />
      <Hero
        // One sentence a line: "You built it." over "We ship it."
        // The last line, "We ship it.", is filled with the brand orange as if it
        // were being poured in: the ink stays only in the W and along the tops.
        heading={hero.heading.split(/(?<=\.)\s+/).map((line, i, lines) => (
          <span
            key={line}
            className={cn(
              "block leading-[1.2]",
              i === lines.length - 1 &&
                "w-fit bg-clip-text pb-[0.08em] text-transparent [background-image:radial-gradient(ellipse_20%_100%_at_0%_0%,var(--foreground)_0%,transparent_65%),linear-gradient(to_bottom,var(--foreground)_0%,var(--foreground)_8%,color-mix(in_oklab,var(--foreground)_70%,transparent)_16%,color-mix(in_oklab,var(--foreground)_40%,transparent)_24%,color-mix(in_oklab,var(--foreground)_15%,transparent)_32%,transparent_42%),linear-gradient(var(--primary),var(--primary))]",
            )}
          >
            {line}
          </span>
        ))}
        variant="xlarge"
        // The words stop short of the chat beside them, leaving room between.
        className="[&_[data-slot=hero-description]]:max-w-[44ch]"
        description={hero.description}
        actions={
          <>
            <AddToAgent size="lg" />
            {sectionsLive && (
              <Button variant="outline" size="lg" asChild>
                <a href={secondaryCta.href}>{secondaryCta.label}</a>
              </Button>
            )}
          </>
        }
        image={picture}
        align="auto"
      />
      </div>

      <Stack gap="spacious">
        <SectionIntro
          variant="xlarge"
          label={gap.label}
          heading={gap.heading}
          description={gap.description}
          align="center"
          // Heading and description each on one line where there is room for
          // them; they still wrap on a phone.
          className="[&_[data-slot=section-intro-description]]:max-w-none [&_[data-slot=section-intro-heading]]:max-w-none"
        />
        <FeatureBlocks />
      </Stack>

      {/* Room after the mosaic, so the next section starts a new thought. */}
      <Stack gap="spacious" className="mt-12">
        <SectionIntro variant="xlarge" align="center" heading={route.heading} description={route.description} />
        <div className="mt-8">
          <RouteTimeline steps={route.steps} icons={stepIcons} lit="share" />
        </div>
      </Stack>

      {/* The solutions wait with the sections (src/lib/sections.ts). */}
      {sectionsLive && (
      <Stack gap="spacious">
        <SectionIntro variant="xlarge" label={also.label} heading={also.heading} />
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
      )}

      <Closing />
    </PageLayoutContent>
  );
}
