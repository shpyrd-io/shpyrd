"use client";

// Two proposals for slide 11 (Giovani, 2026-10-29), beside the slide as it
// is (closing.tsx): the last slide, the call to action, set apart from the
// rest. Both with the shipyard large behind the words, where the bar shows
// it today, but whole, not seen through the bar.
//
// - "orange": the orange band of the site (CtaBand), the yard multiplied into
//   the orange, the words white and the main button black.
// - "white": the site's CTA block (ShipyardCta): the yard on the light page
//   in the orange aurora, the words in the ink and the main button orange.
//
// Under either, the 30-day pilot, a step a week, as the home's road, its
// weeks lit in turn.
import { useEffect, useState } from "react";
import { BarChart3, Check, FolderInput, KeyRound, Users } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { CtaBand } from "@shpyrd/ui/components/cta-band";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { closing } from "@/content/tour-v3";
import { enterpriseContact } from "@/content/offer";
import { DeployButton } from "@/components/deploy-button";
import { ShipyardCta } from "@/components/shipyard-cta";

const weekIcons: Record<string, React.ReactElement> = {
  "week-1": <KeyRound />,
  "week-2": <FolderInput />,
  "week-3": <Users />,
  "week-4": <BarChart3 />,
};

export function ClosingStory({ tone }: { tone: "orange" | "white" }) {
  const heading = (
    <>
      {closing.heading[0]}
      <br />
      {tone === "white" ? (
        <span className="text-primary">{closing.heading[1]}</span>
      ) : (
        closing.heading[1]
      )}
    </>
  );
  const actions = (
    <>
      <DeployButton size="lg" />
      <Button asChild variant="link" size="lg">
        <a href={enterpriseContact.href}>{closing.contact}</a>
      </Button>
    </>
  );
  const note = (
    <span className="flex flex-wrap gap-x-4 gap-y-1">
      {closing.points.map((p) => (
        <span
          key={p}
          className="inline-flex items-center gap-1 [&_svg]:size-3.5"
        >
          <Check />
          {p}
        </span>
      ))}
    </span>
  );

  return (
    <div className="grid gap-10">
      {tone === "orange" ? (
        <CtaBand
          bleed={false}
          // As the slide today, the words with less room above and under
          // them and a wider column; the yard whole and large in the
          // picture's place, multiplied into the orange as the band does.
          className="[&_.cta-band-hero>div>div:first-child]:pt-[26px]! [&_.cta-band-hero>div>div:first-child]:pb-12! @3xl/hero:[&_.cta-band-hero>div]:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]!"
          heading={heading}
          description={closing.lead}
          actions={actions}
          note={note}
          image={
            <div className="aspect-square w-full scale-[1.25] translate-x-[6%] translate-y-[4%]">
              <Shipyard />
            </div>
          }
        />
      ) : (
        <ShipyardCta
          variant="large"
          // Shorter than on a page, so the pilot fits the slide too: the
          // words' column wider (the heading on two lines), the yard's a
          // little narrower and lifted.
          className="-mt-8 mb-0 @3xl/hero:[&>div]:grid-cols-[minmax(0,1.25fr)_minmax(0,1.3fr)]!"
          heading={heading}
          description={closing.lead}
          actions={actions}
          note={note}
        />
      )}

      {/* The pilot, a step a week, as the home's road. */}
      <div className="grid gap-6">
        <p className="text-center text-sm font-medium tracking-wide text-primary uppercase">
          {closing.pilot.title}
        </p>
        <PilotRoute />
      </div>
    </div>
  );
}

// The pilot as slide 4's journey moves (Giovani, 2026-10-29): the home's road,
// its weeks lit one after another, the orange line growing from week to
// week, the week reached orange, those passed edged in orange; after the
// last a rest, and the month again. Still, all lit, for who asked for less
// motion.
const WEEK = 2200;
const REST = 2600;
function PilotRoute() {
  const weeks = closing.pilot.weeks;
  const [at, setAt] = useState(0);
  const [still, setStill] = useState(false);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setStill(true);
      setAt(weeks.length - 1);
    }
  }, [weeks.length]);
  useEffect(() => {
    if (still) return;
    const timer = window.setTimeout(
      () => setAt((a) => (a + 1) % weeks.length),
      at === weeks.length - 1 ? REST : WEEK,
    );
    return () => window.clearTimeout(timer);
  }, [at, still, weeks.length]);
  const share = at / (weeks.length - 1);

  return (
    <ol className="relative grid gap-10 md:grid-cols-4 md:gap-14">
      {/* The line behind the icons, grey, and over it the orange as far as
          the week reached. */}
      <span
        aria-hidden="true"
        className="absolute top-8 right-[12.5%] left-[12.5%] hidden h-px bg-border md:block"
      />
      <span
        aria-hidden="true"
        className="absolute top-8 left-[12.5%] hidden h-px bg-primary transition-[width] duration-900 ease-move md:block"
        style={{ width: `${share * 75}%` }}
      />
      {weeks.map((w, i) => {
        const here = i === at;
        const passed = i < at;
        return (
          <li
            key={w.title}
            className="relative grid justify-items-center gap-4 text-center"
          >
            <span
              className={cn(
                "relative z-10 flex size-16 items-center justify-center rounded-2xl ring-1 transition-[background-color,box-shadow,color] duration-normal ease-move [&_svg]:size-7",
                here
                  ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                  : cn(
                      "bg-background text-primary shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
                      passed ? "ring-primary/50" : "ring-foreground/10",
                    ),
              )}
            >
              {weekIcons[`week-${i + 1}`]}
            </span>
            <div className="grid max-w-[24ch] gap-2">
              <h3
                className={cn(
                  "text-base font-semibold transition-colors duration-normal",
                  here ? "text-primary" : "text-foreground",
                )}
              >
                Week {i + 1} · {w.title}
              </h3>
              <p className="text-sm text-muted-foreground">{w.body}</p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
