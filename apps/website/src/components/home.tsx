import Link from "next/link";
import { ArrowRight, Cloud, Handshake, History, LockKeyhole, Plug, Rocket, SquareTerminal, Undo2, Users, UsersRound } from "lucide-react";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { also, companies, devHunt, gap, hero, peerPush, price, productHunt, route, testimonials } from "@/content/home";
import { Testimonial, TestimonialGrid, TestimonialHighlight } from "@shpyrd/ui/components/testimonial";
import { enterpriseContact, secondaryCta, start } from "@/content/offer";
import { DeployButton } from "@/components/deploy-button";
import { BinaryOcean } from "@/components/binary-ocean";
import { FeatureBlocks } from "@/components/feature-blocks";
import { RouteTimeline } from "@/components/route-timeline";
import { LoopSteps } from "@/components/loop-steps";
import { cn } from "@shpyrd/ui/lib/cn";
import { sectionsLive } from "@/lib/sections";
import { Closing } from "@/components/proposals";
import { River } from "@shpyrd/ui/components/river";
import { AppsYard } from "@/components/tour/apps-yard";
import { ScrollReveal } from "@/components/scroll-reveal";
import { CtaBand } from "@shpyrd/ui/components/cta-band";
import { ShipyardBar } from "@/components/shipyard-bar";
import { signUp } from "@/lib/signup";
import { DevelopersFilm, PartnersFilm, TeamsFilm } from "@/components/audience-films";
import { glass } from "@shpyrd/ui/lib/glass";

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

// The film of each solution (Giovani, 2026-10-20), in place of the
// photographs tried earlier that day, which the site used nowhere else.
const alsoFilms: Record<string, React.ReactElement> = {
  "/solutions/developers": <DevelopersFilm />,
  "/solutions/internal-apps": <TeamsFilm />,
  "/solutions/implementation-partners": <PartnersFilm />,
};

// The icon over each of the three questions, by its id.
const gapIcons: Record<string, React.ReactElement> = {
  where: <Cloud className="size-12" strokeWidth={1.5} />,
  who: <LockKeyhole className="size-12" strokeWidth={1.5} />,
  break: <Undo2 className="size-12" strokeWidth={1.5} />,
};

// The icons of the page, by the id of what they stand beside.
const stepIcons: Record<string, React.ReactElement> = {
  connect: <Plug />,
  deploy: <Rocket />,
  share: <Users />,
  change: <History />,
};

// The way in again, between sections: the hero's button and the line under
// it, centred, for whoever is convinced by what they just read.
function StartAgain({ className }: { className?: string }) {
  return (
    <div className={cn("grid justify-items-center gap-3 text-center", className)}>
      <DeployButton size="lg" label={start.heroLabel} />
      <p className="text-sm text-muted-foreground">{start.note}</p>
    </div>
  );
}

// The customer stories are hidden until there are real testimonials and
// logos (Giovani, 2026-10-20); set to true to show them again.
const storiesLive = false;

// The price section before the closing call, kept but hidden (Giovani,
// 2026-10-20): the price is a line under the call instead.
const priceSectionLive = false;

// A quote with the part that matters lit, found by its words.
function lit(quote: string, highlight: string) {
  const at = quote.indexOf(highlight);
  if (!highlight || at < 0) return quote;
  return (
    <>
      {quote.slice(0, at)}
      <TestimonialHighlight>{highlight}</TestimonialHighlight>
      {quote.slice(at + highlight.length)}
    </>
  );
}

// The quotes are fictional while the copy marks them as a draft: they show in
// development, to design the section, and never in a build.
const showTestimonials = testimonials.draft !== "yes" || process.env.NODE_ENV !== "production";

// "features" replaces what the page says it does (the questions and the
// blocks), for a proposal to try another version of that part.
export function Home({
  picture,
  before,
  features,
  stories,
  order = "default",
  companiesPicture,
  closing = "shipyard",
}: {
  picture: React.ReactNode;
  before?: React.ReactNode;
  features?: React.ReactNode;
  // Replaces the testimonials, for a proposal to try another version of them.
  stories?: React.ReactNode;
  // "conversion": the sections in the order tried for conversion.
  order?: "default" | "conversion";
  // The "For businesses" picture, for a proposal to try another.
  companiesPicture?: React.ReactNode;
  // "band": the closing call on the orange band, the yard in the logo's bar
  // (a test, /proposals/cta-bar, 2026-10-20).
  closing?: "shipyard" | "band";
}) {
  const conversion = order === "conversion";
  // The sections, named so a proposal can try them in another order.
  const featuresSection = (
    <>
      <Stack gap="spacious" className={conversion ? "mt-26" : undefined}>
        {features ?? (
          <>
        <SectionIntro
          variant="xlarge"
          heading={gap.heading}
          description={gap.description}
          align="center"
          // Heading and description each on one line where there is room for
          // them; they still wrap on a phone.
          className="[&_[data-slot=section-intro-description]]:max-w-none [&_[data-slot=section-intro-heading]]:max-w-none"
        />
        {/* The questions that keep the app on the laptop, before the
            answers below. */}
        {/* Out of cards, so they weigh more than the blocks below: a large
            orange icon over each question, as the library's Pillar draws. */}
        <ul className="mt-8 mb-10 grid gap-12 sm:grid-cols-3 sm:gap-10">
          {gap.items.map((q) => (
            <li key={q.id}>
              <Pillar
                align="center"
                icon={gapIcons[q.id]}
                heading={q.title}
                description={q.body}
                className="gap-3 [&_[data-slot=pillar-icon]]:mb-2 [&_[data-slot=pillar-heading]]:text-2xl [&_[data-slot=pillar-heading]]:tracking-tight [&_[data-slot=pillar-description]]:max-w-[36ch]"
              />
            </li>
          ))}
        </ul>
        <FeatureBlocks />
          </>
        )}
        <StartAgain className="mt-12" />
      </Stack>

    </>
  );
  const routeSection = (
    <>
      {/* Room after the mosaic, so the next section starts a new thought. */}
      <Stack gap="spacious" className={conversion ? undefined : "mt-26"}>
        <SectionIntro variant="xlarge" align="center" heading={route.heading} description={route.description} />
        <div className="mt-8">
          {conversion ? (
            // Numbered, on a loop: connect once, then deploy, share and change,
            // again and again (as the developers' page draws its loop).
            <LoopSteps
              className="mx-auto w-full max-w-6xl"
              back={1}
              light="glow"
              again="And again, for every update"
              steps={route.steps.map((step) => ({
                icon: stepIcons[step.id],
                heading: step.title.replace(/\.$/, ""),
                body: step.body,
              }))}
            />
          ) : (
            <RouteTimeline steps={route.steps} icons={stepIcons} lit="share" />
          )}
        </div>
        {/* In the conversion order the way in comes after the features
            instead, a little further on. */}
        {!conversion && <StartAgain className="mt-12" />}
      </Stack>

    </>
  );
  const alsoSection = (
    <>
      {/* The solutions wait with the sections (src/lib/sections.ts). As the
          rest of the page: 168px from the route, a centred title, and each
          solution a card of glass with its icon, as on Pricing. */}
      {sectionsLive && (
      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={also.heading} />
        <ul className="mt-8 grid gap-4 sm:grid-cols-3">
          {also.items.map((a) => (
            <li key={a.href}>
              {/* As the feature cards above: glass, the film on top, the
                  words under it. They are buttons to their pages, so they
                  carry a grey shadow, which under the pointer turns into the
                  orange button's glow (Giovani, 2026-10-20; design/ui
                  button, default), the edge orange. */}
              <Link
                href={a.href}
                className={cn(
                  glass,
                  "group/also flex h-full flex-col gap-4 p-2.5 transition-[box-shadow] duration-300 ease-out hover:ring-primary/50 focus-visible:ring-primary dark:hover:ring-primary/50",
                  "shadow-[0_6px_16px_-6px_rgb(20_20_30/0.18)] hover:shadow-[0_6px_16px_-4px_rgb(255_79_0/0.5)] dark:shadow-[0_6px_16px_-6px_rgb(0_0_0/0.5)] dark:hover:shadow-[0_6px_18px_-4px_rgb(255_79_0/0.6)]",
                )}
              >
                <div aria-hidden="true" className="stage relative aspect-[4/3] rounded-2xl border border-white/80 dark:border-white/10">
                  {alsoFilms[a.href]}
                </div>
                <span className="grid gap-2 px-3.5 pb-3.5">
                  <span className="flex items-center gap-2 text-base font-semibold text-foreground [&_svg]:size-5 [&_svg]:shrink-0 [&>svg:first-child]:text-primary">
                    {alsoIcons[a.href]}
                    {a.title}
                    <ArrowRight aria-hidden="true" className="ml-auto text-muted-foreground transition-[translate,color] group-hover/also:translate-x-0.5 group-hover/also:text-primary" />
                  </span>
                  <span className="text-sm text-muted-foreground">{a.body}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </Stack>
      )}

    </>
  );
  const businessSection = (
    <>
      {/* For companies: their teams' apps, gathered behind one sign-in (the
          manifesto's first slide), and the way to the Small Software
          manifesto. 168px from what comes before, as every section. The
          picture on the left, the words on the right (Giovani, 2026-10-20),
          so it does not repeat the closing call's layout right under it. */}
      <River
        // More room before the closing call (Giovani, 2026-10-20). Its
        // picture's two scenes take the same height (company-apps.tsx), so
        // the room under it is set to match the room over it (at 1466px,
        // about 203px each, from the "Free to start" line to the picture's
        // caption and from its windows to the orange band).
        className="mt-26 mb-12"
        align="end"
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
        picture={companiesPicture ?? <AppsYard className="aspect-[5/4]" />}
      />

    </>
  );
  const priceSection = (
    <>
      {/* What it costs, answered on the page, before the last way in. */}
      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={price.heading} description={price.description} />
        <div className="flex justify-center">
          <Button asChild size="lg" variant="outline" iconEnd={<ArrowRight />}>
            <Link href={price.link.href}>{price.link.label}</Link>
          </Button>
        </div>
      </Stack>

    </>
  );
  const testimonialsSection = (
    <>
      {/* People who shipped theirs, last before the closing call (Giovani,
          2026-10-19): one big quote, then a row of short ones.
          168px from the route, as every section. */}
      {stories ? <div className="mt-26">{stories}</div> : showTestimonials && (
        <Stack gap="spacious" className="mt-26">
          <SectionIntro variant="xlarge" align="center" heading={testimonials.heading} />
          {/* Each with their face (the initials keep its place until the
              photo comes), and their company's logo when a reader would know it. */}
          <Testimonial
            className="mt-8"
            quote={lit(testimonials.feature.quote, testimonials.feature.highlight)}
            name={testimonials.feature.name}
            role={`${testimonials.feature.role}, ${testimonials.feature.company}`}
            avatar={testimonials.feature.photo ?? ""}
            logo={testimonials.feature.logo || undefined}
            company={testimonials.feature.company}
          />
          <TestimonialGrid className="mt-12">
            {testimonials.items.map((t) => (
              <Testimonial
                key={t.name}
                variant="card"
                quote={lit(t.quote, t.highlight)}
                name={t.name}
                role={`${t.role}, ${t.company}`}
                avatar={t.photo ?? ""}
                logo={t.logo || undefined}
                company={t.company}
              />
            ))}
          </TestimonialGrid>
        </Stack>
      )}

    </>
  );

  return (
    <PageLayoutContent width="xlarge" padding="normal" className="grid content-start gap-16 py-8">
      {before}
      {/* What comes into view rises into place (after apple.com). */}
      <ScrollReveal />
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
        // The actions stand 40px under the words, the same 40px that
        // separates the buttons from the badges.
        // The back windows of the picture lean left over the column of words
        // (at 1024px they reached the end of the description's lines), so
        // the words sit above them.
        className="[&_[data-slot=hero-description]]:max-w-[44ch] [&_[data-slot=hero-actions]]:mt-5 [&_[data-slot=hero-description]]:relative [&_[data-slot=hero-description]]:z-30 [&_[data-slot=hero-actions]]:relative [&_[data-slot=hero-actions]]:z-30"
        description={hero.description}
        actions={
          // The buttons, and the launch badges under them, both on the left
          // edge of the words beside the picture, centred when stacked.
          <div className="flex flex-col items-center gap-10 @3xl/hero:items-start">
            <div className="grid justify-items-center gap-3 @3xl/hero:justify-items-start">
              <div className="flex flex-wrap items-start justify-center gap-3 @3xl/hero:justify-start">
                <DeployButton size="lg" label={start.heroLabel} />
                {sectionsLive && (
                  <Button variant="outline" size="lg" asChild>
                    <a href={secondaryCta.href}>{secondaryCta.label}</a>
                  </Button>
                )}
              </div>
              {/* What trying costs, under the button: nothing. */}
              <p className="text-sm text-muted-foreground">{start.note}</p>
            </div>
            <div className="flex flex-wrap items-center justify-center gap-3 @3xl/hero:justify-start">
              <a href={productHunt.href} target="_blank" rel="noopener noreferrer" className="shrink-0">
                {/* The badge in the site's theme, a light one and a dark one: Product Hunt's own, kept in public/badges without the upvote count until the launch. */}
                {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
                <img alt={productHunt.alt} width={202} height={54} src={productHunt.image} className="h-12 w-auto dark:hidden" />
                {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
                <img alt={productHunt.alt} width={202} height={54} src={productHunt.imageDark} className="hidden h-12 w-auto dark:block" />
              </a>
              <a href={peerPush.href} target="_blank" rel="noopener noreferrer" className="shrink-0">
                {/* The same height as the Product Hunt badge beside it. */}
                {/* eslint-disable-next-line @next/next/no-img-element -- PeerPush serves the badge */}
                <img src={peerPush.image} alt={peerPush.alt} width={191} height={54} className="h-12 w-auto" />
              </a>
              {/* The award from DevHunt (2026-10-20). The three badges 48px
                  high rather than their 54px, so they keep to one row beside
                  the hero's picture. */}
              <a href={devHunt.href} target="_blank" rel="noopener noreferrer" title="shpyrd on DevHunt" className="shrink-0">
                {/* eslint-disable-next-line @next/next/no-img-element -- DevHunt serves the badge */}
                <img src={devHunt.image} alt={devHunt.alt} width={220} height={54} className="h-12 w-auto" />
              </a>
            </div>
          </div>
        }
        image={picture}
        align="auto"
      />
      </div>

      {conversion ? (
        // The order for conversion (Giovani, 2026-10-19): how it works, then
        // what it does, the other builders it is for, the proof and a way in,
        // the companies, and the price as the last question before the last
        // way in.
        <>
          {routeSection}
          {featuresSection}
          {alsoSection}
          {/* The stories wait, kept, until there are real ones (Giovani,
              2026-10-20); the way in that followed them closes the
              solutions instead. */}
          {storiesLive && testimonialsSection}
          <StartAgain />
          {businessSection}
          {/* The price is no longer a section of its own before the
              closing call (Giovani, 2026-10-20): it answered "how much?"
              with a second big heading and a way out to Pricing right
              before "Get started". It is now a line under the call. */}
          {priceSectionLive && priceSection}
        </>
      ) : (
        <>
          {featuresSection}
          {routeSection}
          {alsoSection}
          {businessSection}
          {priceSection}
          {testimonialsSection}
        </>
      )}
      {closing === "band" ? (
        // Out to the window's sides and cut there: the yard's bar runs past
        // the site's edge and must not widen the page (the words keep their
        // column: the padding gives back what the margin takes).
        <div className="relative isolate mx-[calc(50%-50vw)] overflow-x-clip px-[calc(50vw-50%)]">
        {/* A soft shadow round the whole band, edge to edge (Giovani,
            2026-10-20), so the white of the yard's bar does not meet the
            page's light grey edge to edge. Outside the band, which clips
            what reaches past its top. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute top-0 -bottom-6 left-1/2 -z-10 w-screen -translate-x-1/2 shadow-[0_0_48px_6px_rgb(60_20_0/0.28)]"
        />
        <CtaBand
          // The yard's bar is white on the orange, not multiplied into it.
          // The heading a step over the sections' (48px → 60px), for impact
          // (Giovani, 2026-10-20): the library's next size, as on its pages.
          className="[&_[data-slot=hero-image]]:mix-blend-normal! lg:[&_[data-slot=hero-heading]]:text-6xl! [&_.cta-band-hero>div>div:first-child]:pb-14!"
          heading="Start on shpyrd cloud"
          description="Sign up, connect your agent, and ship your first app. Or run it yourself: shpyrd is open source under MPL-2.0, and the quick start takes you from nothing to a deployed app."
          actions={
            <>
              <Button size="lg" asChild>
                <a href={signUp}>Get started</a>
              </Button>
              <Button variant="link" size="lg" asChild>
                <a href="/docs/installation">Run it yourself</a>
              </Button>
            </>
          }
          note={
            <>
              {start.note}{" "}
              <Link href={price.link.href} className="font-medium underline underline-offset-4">
                {price.link.label} →
              </Link>
            </>
          }
          image={<ShipyardBar />}
        />
        </div>
      ) : (
        <Closing
          note={
            conversion ? (
              <>
                {start.note}{" "}
                <Link href={price.link.href} className="font-medium text-primary underline-offset-4 hover:underline">
                  {price.link.label} →
                </Link>
              </>
            ) : undefined
          }
        />
      )}
    </PageLayoutContent>
  );
}
