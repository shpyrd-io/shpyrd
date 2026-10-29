"use client";

// A proposal for slide 10 (Giovani, 2026-10-29), beside the slide as it is
// (audiences.tsx). What it says: everybody wins, even whoever says "no", IT.
// So the slide turns, as slide 3 does, from today to with shpyrd: the four
// people of the story (whoever built, IT, engineering, finance), each with
// what hurts today, crossed in red, and what they get, ticked, each gain
// with the slide that showed it, a click away. IT's badge turns from "Says
// no" to "Says yes". It turns by itself until a side is chosen.
import { useEffect, useState } from "react";
import { Check, Pause, Play, Users, X } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { audiences } from "@/content/tour-v3";
import { PersonMark, type Look } from "./person-mark";
import { Intro, Label, panel, rise, mouseOnly } from "./parts";

type Side = "today" | "shpyrd";

// The four, as the story met them; each gain with the slide that showed it
// (its place in the deck, from 0).
const groups: {
  name: string;
  who: string;
  look: Look;
  quote: string;
  today: string[];
  gains: { text: string; slide: number; what: string }[];
}[] = [
  {
    name: "Builders",
    who: "Lucas Prado",
    look: "man",
    quote: "“I just want the app to work and my team to use it.”",
    today: [
      "Waits weeks for a ticket",
      "Builds sign-in and permissions by hand",
      "Shares a link that only works on a laptop",
    ],
    gains: [
      { text: "Ship without a ticket", slide: 8, what: "freedom" },
      {
        text: "Sign-in and permissions already done",
        slide: 5,
        what: "front door",
      },
      { text: "One sentence to share it", slide: 3, what: "the journey" },
    ],
  },
  {
    name: "IT and security",
    who: "Julia Reis",
    look: "woman-bun",
    quote: "“I can’t block everything, but I can’t be blind either.”",
    today: [
      "Apps nobody told IT about",
      "A sign-in of its own in each one",
      "No trail of who did what",
    ],
    gains: [
      {
        text: "Company sign-in and roles on every app",
        slide: 5,
        what: "front door",
      },
      { text: "One list and one audit trail", slide: 8, what: "in view" },
      { text: "The end of shadow IT", slide: 2, what: "x-ray" },
    ],
  },
  {
    name: "Engineering",
    who: "Rafa Lima",
    look: "man-beard",
    quote: "“I don’t want to babysit 40 apps someone else wrote.”",
    today: [
      "Babysits apps someone else wrote",
      "Builds a platform nobody asked for",
      "Rewrites them to fit one stack",
    ],
    gains: [
      { text: "No platform to build or run", slide: 7, what: "where it runs" },
      {
        text: "Logs, metrics and rollback for each app",
        slide: 8,
        what: "the trail",
      },
      { text: "Any stack, from the repository", slide: 4, what: "the flow" },
    ],
  },
  {
    name: "Finance",
    who: "Marina Costa",
    look: "woman",
    quote: "“Why are 50 databases on at three in the morning?”",
    today: [
      "Pays for databases on all night",
      "No cost per app or per team",
      "A platform team to hire",
    ],
    gains: [
      { text: "Apps and databases that sleep", slide: 6, what: "sleep" },
      { text: "Cost per app and per team", slide: 8, what: "the costs" },
      { text: "No platform team to hire", slide: 7, what: "where it runs" },
    ],
  },
];
const HOLD = 7000;
const stepButton = "hover:border-primary/40 hover:text-primary";

// The words as on slides 7 to 9: captions 12 px muted, values 14 px in the
// foreground. The cards rest on the slide: no shadow; under the pointer an
// orange edge only.
export function AudiencesStory({ go }: { go?: (slide: number) => void } = {}) {
  const [side, setSide] = useState<Side>("today");
  const [moving, setMoving] = useState(true);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setMoving(false);
      setSide("shpyrd");
    }
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(
      () => setSide((s) => (s === "today" ? "shpyrd" : "today")),
      HOLD,
    );
    return () => window.clearTimeout(timer);
  }, [moving, side]);
  const ours = side === "shpyrd";

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Users />}>{audiences.label}</Label>}
        heading={audiences.heading}
        lead={audiences.lead}
      />

      {/* Today or with shpyrd, on the design system's pill, as slide 3. */}
      <div className="flex justify-center">
        <Tabs
          value={side}
          onValueChange={(v) => {
            setMoving(false);
            setSide(v as Side);
          }}
        >
          <TabsList aria-label="Today or with shpyrd" variant="pill">
            <TabsTrigger
              {...mouseOnly}
              value="today"
              icon={
                <span
                  className={cn(
                    "size-2! rounded-full transition-colors",
                    ours ? "bg-muted-foreground/50" : "bg-destructive",
                  )}
                />
              }
            >
              Today
            </TabsTrigger>
            <TabsTrigger
              {...mouseOnly}
              value="shpyrd"
              icon={<LogoMark className="size-5!" />}
            >
              With shpyrd
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {groups.map((g, i) => (
          <div
            key={g.name}
            style={{ animationDelay: `${360 + i * 120}ms` }}
            className={cn(
              panel,
              rise,
              "relative grid content-start gap-4 p-5 transition-[box-shadow] duration-normal",
              // Their edge says the side (Giovani, 2026-10-29): red today,
              // green with shpyrd.
              ours
                ? "ring-success/60 dark:ring-success/60"
                : "ring-destructive/50 dark:ring-destructive/50",
            )}
          >
            {/* For IT, whether it says no or yes, in the card's top right
                corner (Giovani, 2026-10-29). */}
            {g.name === "IT and security" && (
              <StatusBadge
                type={ours ? "success" : "error"}
                className="absolute top-4 right-4"
              >
                {ours ? "Says yes" : "Says no"}
              </StatusBadge>
            )}
            {/* Who, as slide 6 shows its people (Giovani, 2026-10-29): the
                route's box with the person drawn in it, large, in the middle;
                their name under it, then their department (for IT, whether
                it says no or yes), then what they say in a bubble out of the
                box, then the list. */}
            {/* On a phone the person beside their name, so the four cards
                stacked stay short. */}
            <div className="grid justify-items-center gap-3 text-center max-sm:flex max-sm:items-center max-sm:gap-4 max-sm:text-left">
              {/* The person alone, as large as the box was (Giovani,
                  2026-10-29): no tile, no shadow. */}
              <PersonMark look={g.look} className="size-20 max-sm:size-14" />
              <div className="grid justify-items-center gap-1 max-sm:justify-items-start">
                <span className="text-lg font-semibold text-foreground">
                  {g.who}
                </span>
                <span className="flex h-5 items-center gap-2">
                  <span className="text-sm text-muted-foreground">
                    {g.name}
                  </span>
                </span>
              </div>
            </div>
            {/* What they say, in a white speech bubble with its shadow (Giovani,
                2026-10-29), out of their box: its tail
                up to the box's middle. */}
            <p className="relative mt-1 rounded-xl bg-background px-3 py-2.5 text-center text-sm text-primary italic [filter:drop-shadow(0_1px_2px_rgb(20_20_30/0.08))_drop-shadow(0_6px_14px_rgb(20_20_30/0.08))] dark:bg-accent before:absolute before:-top-2 before:left-1/2 max-sm:before:left-7 before:h-2 before:w-3 before:-translate-x-1/2 before:bg-background dark:before:bg-accent before:[clip-path:polygon(50%_0,100%_100%,0_100%)]">
              {g.quote}
            </p>

            {/* Both sides in one cell, only one shown, so the cards keep
                their height as the slide turns. */}
            <div className="grid pt-1">
              <ul
                aria-hidden={ours ? true : undefined}
                className={cn(
                  "grid content-start gap-3 [grid-area:1/1]",
                  ours && "invisible",
                )}
              >
                {g.today.map((t, k) => (
                  <li
                    key={`${side}-${t}`}
                    className={cn(
                      "flex gap-2.5 text-sm text-muted-foreground",
                      rise,
                    )}
                    style={{ animationDelay: `${k * 120}ms` }}
                  >
                    <X
                      className="mt-0.5 size-4 shrink-0 text-destructive"
                      strokeWidth={2.5}
                    />
                    {t}
                  </li>
                ))}
              </ul>
              <ul
                aria-hidden={ours ? undefined : true}
                className={cn(
                  "grid content-start gap-3 [grid-area:1/1]",
                  !ours && "invisible",
                )}
              >
                {g.gains.map((t, k) => (
                  <li
                    key={`${side}-${t.text}`}
                    className={cn("grid gap-1 text-sm text-foreground", rise)}
                    style={{ animationDelay: `${k * 120}ms` }}
                  >
                    <span className="flex gap-2.5">
                      <Check
                        className="mt-0.5 size-4 shrink-0 text-success"
                        strokeWidth={2.5}
                      />
                      {t.text}
                    </span>
                    {/* The slide that showed it, a click away. */}
                    <button
                      type="button"
                      {...mouseOnly}
                      disabled={!go}
                      onClick={() => go?.(t.slide)}
                      className="ml-6.5 w-fit rounded-md text-xs text-muted-foreground transition-colors enabled:cursor-pointer enabled:hover:text-primary"
                    >
                      Slide {t.slide + 1} · {t.what} →
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        ))}
      </div>

      {/* As the other slides: play or pause the turn. */}
      <div className="-mt-2 flex items-center justify-center">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => setMoving((m) => !m)}
          aria-label={moving ? "Pause" : "Play"}
        >
          {moving ? (
            <Pause className="fill-current" />
          ) : (
            <Play className="translate-x-px fill-current" />
          )}
        </Button>
      </div>
    </div>
  );
}
