import { ArrowRight, Ban, DoorOpen, Rocket, Undo2 } from "lucide-react";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Hero } from "@shpyrd/ui/components/hero";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundariesForStep } from "@shpyrd/content/site/boundaries";
import { intro, runs, steps } from "@shpyrd/content/site/sharing";
import { Boundaries } from "@/components/boundaries";
import { ContainerCircle } from "@/components/container-helix";
import { VerticalRoute } from "@/components/vertical-route";
import { pageSections } from "@/lib/page";

export const metadata = { title: "Getting Started", description: intro.lead };

// The mark of each step on the route.
const icons: Record<string, React.ReactElement> = {
  publish: <Rocket />,
  open: <DoorOpen />,
  denied: <Ban />,
  operate: <Undo2 />,
};

export default function HowSharingWorks() {
  return (
    <>
    {/* The page's background: the container circle (container-helix.tsx),
        fixed behind it, turning as the page is scrolled. Outside the page's
        grid, so it takes none of its room. The spiral it replaced stays at
        /proposals/background. */}
    <ContainerCircle />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero variant="page" align="center" heading={intro.title} description={intro.lead} />

      {/* Four steps in the order they happen, as the home page's route
          standing up (vertical-route.tsx): the line draws itself down them as
          the reader reaches them. Each step in the glass of the home page's
          blocks, so it reads over the container spiral behind. */}
      <VerticalRoute
        className="mx-auto w-full max-w-5xl"
        flush
        steps={steps.map((step, index) => {
          const limits = boundariesForStep(step.step);
          // Step 4 has two limits: the second goes under its picture, so the
          // two columns end together instead of leaving the picture's side
          // empty.
          const under = step.id === "operate" ? limits.slice(1) : [];
          const beside = step.id === "operate" ? limits.slice(0, 1) : limits;
          return {
            icon: icons[step.id] ?? <Rocket />,
            // On this page, every step in orange.
            lit: true,
            children: (
              <div
                // The panel's top level with its icon's, and "Step N" level
                // with the icon: 18px over it centres it in the icon's 56px.
                className={cn(glass, "grid gap-6 p-6 pt-[18px] sm:p-8 sm:pt-[18px] lg:grid-cols-2 lg:items-start lg:gap-12")}
              >
                <Stack gap="normal">
                  <p className="text-sm leading-5 font-medium text-primary">Step {index + 1}</p>
                  <h2 className="max-w-[18ch] text-2xl font-semibold text-foreground">{step.title}</h2>
                  <p className="max-w-prose text-muted-foreground">{step.body}</p>
                  {beside.length > 0 && <Boundaries items={beside} />}
                </Stack>
                {/* The picture as far from the panel's top as from its side and
                    foot (32px): the panel's top padding is 18px, for "Step N",
                    so it is 14px lower. */}
                <div className="grid gap-8 lg:pt-[14px]">
                  {step.screenshot.exists ? (
                    /* eslint-disable-next-line @next/next/no-img-element */
                    <img
                      src={step.screenshot.src}
                      alt={step.screenshot.alt}
                      loading="lazy"
                      // As the home page's pictures (feature-blocks.tsx): a
                      // 10px corner, an edge of white, a soft deep shadow;
                      // turned to dark on the dark page, the orange kept.
                      className="w-full rounded-[10px] border border-white/80 shadow-[0_6px_16px_rgb(25_25_40/0.07)] dark:border-white/10 dark:invert dark:hue-rotate-180"
                    />
                  ) : (
                    <Blankslate
                      border
                      title="Screenshot pending"
                      description={step.screenshot.note ?? step.screenshot.alt}
                    />
                  )}
                  {under.length > 0 && <Boundaries items={under} />}
                </div>
              </div>
            ),
          };
        })}
      />

      {/* Where it runs: its own words, centred as the home page's sections
          are, with no box around anything, so it is not read as one more step
          of the route above. */}
      <div className="grid justify-items-center gap-6 text-center">
        <SectionIntro align="center" variant="xlarge" className="w-full" heading={runs.title} description={runs.body} />
        {/* The limit read with it, centred too: no rule at its side. */}
        <div className="w-full max-w-4xl [&_li]:border-l-0 [&_li]:pl-0 [&_p]:mx-auto">
          <Boundaries items={boundariesForStep(runs.step)} />
        </div>
        <Button variant="outline" size="lg" asChild iconEnd={<ArrowRight />}>
          <a href={runs.link.href}>{runs.link.label}</a>
        </Button>
      </div>
    </PageLayoutContent>
    </>
  );
}
