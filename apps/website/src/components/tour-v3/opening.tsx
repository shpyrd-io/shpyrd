"use client";

// Slide 1: welcome to the Small Software era. Beside it, the home's "For
// businesses" film (company-apps.tsx, 2026-10-20): the apps in four
// scattered windows, then flying into the company's one.
import {
  ArrowRight,
  Compass,
  Info,
  KeyRound,
  Moon,
  ShieldCheck,
  Users,
} from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { opening } from "@/content/tour-v3";
import { cn } from "@shpyrd/ui/lib/cn";
import { Label, Lead, panel } from "./parts";
import { CompanyApps } from "@/components/company-apps";

const pointIcons = [
  <KeyRound key="k" />,
  <Users key="u" />,
  <Moon key="m" />,
  <ShieldCheck key="s" />,
];

// A key as it is on the keyboard, named in words where a sign would not be
// read (the space bar).
function Key({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-6 min-w-6 items-center justify-center rounded-md bg-background px-1.5 font-sans text-xs font-medium text-foreground shadow-[0_1px_0_rgb(0_0_0/0.08)] ring-1 ring-foreground/15">
      {children}
    </kbd>
  );
}

export function Opening({ next }: { next: () => void }) {
  return (
    <div className="grid gap-14">
      {/* The words from the top, as on every slide (Giovani, 2026-10-27),
          the label and the heading where the next slides have theirs. */}
      <div className="grid items-start gap-12 lg:grid-cols-[1.1fr_1fr]">
        <div className="grid gap-6">
          {/* The label, heading and line 16px apart, as every slide's. */}
          <div className="grid gap-4">
            {/* A compass, a tour's icon, as every slide's label has one (Giovani, 2026-10-27). */}
            <Label icon={<Compass />}>{opening.label}</Label>
            <h1 className="font-heading text-5xl leading-[1.04] font-semibold tracking-tight text-balance sm:text-6xl">
              {opening.heading[0]}{" "}
              <span className="text-primary">{opening.heading[1]}</span>{" "}
              {opening.heading[2]}
            </h1>
            <Lead>{opening.lead}</Lead>
          </div>
          <ul className="flex flex-wrap gap-2">
            {opening.points.map((point, i) => (
              <li
                key={point}
                className="inline-flex items-center gap-2 rounded-lg bg-background px-3 py-1.5 text-sm ring-1 ring-foreground/8 [&_svg]:size-4 [&_svg]:text-primary"
              >
                {pointIcons[i]}
                {point}
              </li>
            ))}
          </ul>
          <div>
            <Button size="lg" onClick={next} iconEnd={<ArrowRight />}>
              {opening.start}
            </Button>
          </div>
        </div>

        <CompanyApps />
      </div>

      {/* How to move through the deck, said in words (Giovani,
          2026-10-27): the keys at their own size, each with what it
          does, and that the buttons at the foot do the same; on one
          line, centred under the slide, with room above it. */}
      {/* In a white glass box with an "i" beside it (Giovani, 2026-10-27).
          Not on a phone: no keys there; the arrows at the foot. */}
      <p
        className={cn(
          panel,
          "mx-auto flex w-fit items-center gap-1.5 px-4 py-2.5 text-sm whitespace-nowrap text-muted-foreground max-md:hidden",
        )}
      >
        <Info
          aria-hidden="true"
          className="mr-1 size-4 shrink-0 text-primary"
        />
        Press
        <Key>→</Key>
        or the
        <Key>Space</Key>
        bar for the next slide,
        <Key>←</Key>
        to go back. Or use the arrows at the bottom.
      </p>
    </div>
  );
}
