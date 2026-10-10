import Link from "next/link";
import { ArrowRight, Handshake, History, Plug, Rocket, SquareTerminal, Users, UsersRound } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { glass } from "@shpyrd/ui/lib/glass";
import { also, companies, gap, hero, route } from "@shpyrd/content/site/home";
import { enterpriseContact, secondaryCta } from "@shpyrd/content/site/offer";
import { DeployButton } from "@/components/deploy-button";
import { BinaryOcean } from "@/components/binary-ocean";
import { FeatureBlocks } from "@/components/feature-blocks";
import { RouteTimeline } from "@/components/route-timeline";
import { cn } from "@shpyrd/ui/lib/cn";
import { sectionsLive } from "@/lib/sections";
import { Closing } from "@/components/proposals";
import { River } from "@shpyrd/ui/components/river";
import { AppsYard } from "@/components/tour/apps-yard";

// The homepage, for the person who built an app with AI and cannot yet put it in
// front of anyone (homepage proposal 1, chosen 2026-09-30). It starts where
// their app is today: working, on their laptop, for nobody else. The other
// people who choose shpyrd have a page each, reached from "Also for" and the
// Solutions menu.
//
// The picture beside the heading is a part the page is given: "/" gives the
// chat, and a proposal can give another.

// The icon of each solution, as in the header's menu.
const alsoIcons: Record<string, React.ReactElement> = {
  "/solutions/developers": <SquareTerminal />,
  "/solutions/internal-apps": <UsersRound />,
  "/solutions/implementation-partners": <Handshake />,
};

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
        // The last line, "We ship it.", in the brand orange, plain.
        heading={hero.heading.split(/(?<=\.)\s+/).map((line, i, lines) => (
          <span
            key={line}
            className={cn(
              "block leading-[1.2]",
              i === lines.length - 1 && "text-primary",
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
            <DeployButton size="lg" />
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
      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={route.heading} description={route.description} />
        <div className="mt-8">
          <RouteTimeline steps={route.steps} icons={stepIcons} lit="share" />
        </div>
      </Stack>

      {/* The solutions wait with the sections (src/lib/sections.ts). As the
          rest of the page: 168px from the route, a centred title, and each
          solution a card of glass with its icon, as on Pricing. */}
      {sectionsLive && (
      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={also.heading} />
        <ul className="mt-8 grid gap-4 sm:grid-cols-3">
          {also.items.map((a) => (
            <li key={a.href}>
              <Link
                href={a.href}
                className={cn(
                  glass,
                  // A link: its edge turns orange under the pointer, with a soft glow, as
                  // a plan on Pricing does.
                  "group/also grid h-full content-start gap-2 p-6 transition-[box-shadow] duration-300 hover:ring-primary dark:hover:ring-primary hover:shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_10px_30px_rgb(20_20_30/0.06),0_0_28px_-6px_rgb(255_79_0/0.45)] dark:hover:shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_10px_30px_rgb(0_0_0/0.6),0_0_28px_-6px_rgb(255_79_0/0.5)] focus-visible:ring-primary",
                )}
              >
                <span className="flex items-center gap-2 text-base font-semibold text-foreground [&_svg]:size-[18px] [&_svg]:shrink-0 [&_svg]:text-primary">
                  {alsoIcons[a.href]}
                  {a.title}
                  <ArrowRight
                    aria-hidden="true"
                    className="ml-auto text-muted-foreground transition-[translate,color] group-hover/also:translate-x-0.5 group-hover/also:text-primary"
                  />
                </span>
                <span className="text-sm text-muted-foreground">{a.body}</span>
              </Link>
            </li>
          ))}
        </ul>
      </Stack>
      )}

      {/* For companies: their teams' apps, gathered behind one sign-in (the
          manifesto's first slide), and the way to the Small Software
          manifesto. 168px from what comes before, as every section. */}
      <River
        className="mt-26"
        heading={
          <span className="grid gap-3">
            <span className="text-sm font-medium tracking-wide text-primary uppercase">{companies.label}</span>
            {companies.heading}
          </span>
        }
        description={companies.description}
        link={
          <span className="flex flex-wrap gap-3">
            <Button asChild size="lg" iconEnd={<ArrowRight />}>
              <Link href={companies.manifesto.href}>{companies.manifesto.label}</Link>
            </Button>
            <Button asChild size="lg" variant="outline">
              <Link href={enterpriseContact.href}>{enterpriseContact.label}</Link>
            </Button>
          </span>
        }
        picture={<AppsYard className="aspect-[5/4]" />}
      />

      <Closing />
    </PageLayoutContent>
  );
}
