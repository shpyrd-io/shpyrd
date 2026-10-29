"use client";

// A proposal for slide 11 read as the deck's conversion (Giovani,
// 2026-10-29), beside the white close in the deck (closing-story.tsx). The
// reader has seen ten slides of proof and decides here, for a company:
//
// - the pilot is the main call to action ("Start a 30-day pilot"), starting
//   alone the second ("Start free");
// - the lead recalls what was shown rather than listing features, and three
//   figures of the deck stand under it as its proof;
// - what lowers the risk sits by the buttons; the awards of the home's hero
//   under them, small;
import {
  BarChart3,
  Cloud,
  Code2,
  FolderGit2,
  FolderInput,
  KeyRound,
  Users,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { closing } from "@/content/tour-v3";
import { enterpriseContact } from "@/content/offer";
import { devHunt, peerPush, productHunt } from "@/content/home";
import { signUp } from "@/lib/signup";
import { RouteTimeline } from "@/components/route-timeline";
import { ShipyardCta } from "@/components/shipyard-cta";
import { rise } from "./parts";

// Figures true for every customer, as the product makes them (Giovani,
// 2026-10-29: the deck's own were made up for its story).
const proof = [
  { value: "100%", label: ["behind your", "sign-in"] },
  { value: "0", label: ["secrets in", "the code"] },
  { value: "$0", label: ["while an", "app sleeps"] },
];
// What lowers the risk, as seals of the awards' kind beside them: an icon,
// a small line over the words.
const seals = [
  { icon: <FolderGit2 />, over: "From your repository", name: "No rewrites" },
  {
    icon: <Cloud />,
    over: "Runs where you choose",
    name: "Your cloud or ours",
  },
  { icon: <Code2 />, over: "The code is public", name: "Open source" },
];

export function ClosingConvert() {
  return (
    <div className="grid gap-8">
      <ShipyardCta
        variant="large"
        // As the white close in the deck: shorter than on a page. The lead,
        // the figures, the buttons and the seals 32 px apart each (Giovani,
        // 2026-10-29; the column's own 20 px and 12 more). The yard larger
        // (1.22), its top over the heading's, its ship near the pilot.
        className="mb-0 md:-mt-8 @3xl/hero:[&>div]:grid-cols-[minmax(0,1.25fr)_minmax(0,1.3fr)]! [&_[data-slot=hero-actions]]:mt-3 [&_[data-slot=hero-note]]:mt-3 md:[&_[data-slot=hero-image]]:scale-[1.22] [&_[data-slot=hero-actions]]:animate-[reveal-rise_1000ms_cubic-bezier(0.2,0.7,0.2,1)_both] [&_[data-slot=hero-actions]]:[animation-delay:360ms] [&_[data-slot=hero-image]]:animate-[reveal-rise_1400ms_cubic-bezier(0.2,0.7,0.2,1)_both] [&_[data-slot=hero-image]]:[animation-delay:200ms]"
        // As the slide opens its parts come in one after another, 120 ms
        // apart, as on every slide (Giovani, 2026-10-29): the heading, the
        // lead, the figures, the buttons, the seals, the awards; the yard
        // with them, slower.
        heading={
          <span className={cn("block", rise)}>
            {closing.heading[0]}
            <br />
            <span className="text-primary">{closing.heading[1]}</span>
          </span>
        }
        description={
          <span className="grid gap-8">
            <span className={rise} style={{ animationDelay: "120ms" }}>
              Every app behind your sign-in, in one list, paying only for what
              runs. Try it on three of yours.
            </span>
            {/* The proof, the deck's figures. */}
            <span
              className={cn(
                "flex flex-wrap gap-x-8 gap-y-3 max-md:justify-center",
                rise,
              )}
              style={{ animationDelay: "240ms" }}
            >
              {proof.map((p) => (
                // The figure, and beside it on its right its line in two
                // (Giovani, 2026-10-29).
                <span key={p.value} className="flex items-center gap-2">
                  {/* A fine bar before each, a light grey, close to its figure, parting
                      one from the next (Giovani, 2026-10-29). */}
                  <span
                    aria-hidden="true"
                    className="-mr-0.5 h-7 w-px rounded-full bg-foreground/20"
                  />
                  <span className="font-heading text-2xl font-semibold text-foreground tabular-nums">
                    {p.value}
                  </span>
                  <span className="grid text-xs leading-tight text-muted-foreground">
                    <span>{p.label[0]}</span>
                    <span>{p.label[1]}</span>
                  </span>
                </span>
              ))}
            </span>
          </span>
        }
        actions={
          <>
            {/* The pilot first: the decision is the company's. */}
            <Button asChild size="lg">
              <a href={enterpriseContact.href}>Start a 30-day pilot</a>
            </Button>
            <Button asChild size="lg" variant="outline">
              <a href={signUp}>Start free</a>
            </Button>
          </>
        }
        note={
          <span className="grid gap-2.5">
            {/* The seals, of the awards' kind (Giovani, 2026-10-29): as high
                as the badges under them, white, the badges' own edge (#e5e5e5,
                the ink at 12%), the icon in orange, a small line over the
                words; 10 px between them every way. */}
            <span
              className={cn(
                "flex flex-wrap items-center gap-2.5 max-md:justify-center",
                rise,
              )}
              style={{ animationDelay: "480ms" }}
            >
              {seals.map((x) => (
                <span
                  key={x.name}
                  className="inline-flex h-9 shrink-0 items-center gap-2 rounded-lg bg-background px-2.5 ring-1 ring-foreground/12 dark:bg-card [&_svg]:size-5 [&_svg]:text-primary"
                >
                  {x.icon}
                  <span className="grid text-left leading-none">
                    <span className="text-[8px] font-semibold tracking-wider text-muted-foreground uppercase">
                      {x.over}
                    </span>
                    <span className="mt-0.5 text-[13px] font-semibold text-foreground">
                      {x.name}
                    </span>
                  </span>
                </span>
              ))}
            </span>
            {/* The awards of the home's hero, smaller: proof that others
                have tried it, kept quiet beside the decision. */}
            <span
              className={cn(
                "flex flex-wrap items-center gap-2.5 max-md:justify-center",
                rise,
              )}
              style={{ animationDelay: "600ms" }}
            >
              <a
                href={productHunt.href}
                target="_blank"
                rel="noopener noreferrer"
                className="shrink-0"
              >
                {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
                <img
                  alt={productHunt.alt}
                  width={202}
                  height={54}
                  src={productHunt.image}
                  className="h-9 w-auto dark:hidden"
                />
                {/* eslint-disable-next-line @next/next/no-img-element -- Product Hunt serves the badge */}
                <img
                  alt={productHunt.alt}
                  width={202}
                  height={54}
                  src={productHunt.imageDark}
                  className="hidden h-9 w-auto dark:block"
                />
              </a>
              <a
                href={peerPush.href}
                target="_blank"
                rel="noopener noreferrer"
                className="shrink-0"
              >
                {/* eslint-disable-next-line @next/next/no-img-element -- PeerPush serves the badge */}
                <img
                  src={peerPush.image}
                  alt={peerPush.alt}
                  width={191}
                  height={54}
                  className="h-9 w-auto"
                />
              </a>
              <a
                href={devHunt.href}
                target="_blank"
                rel="noopener noreferrer"
                title="shpyrd on DevHunt"
                className="shrink-0"
              >
                {/* eslint-disable-next-line @next/next/no-img-element -- DevHunt serves the badge */}
                <img
                  src={devHunt.image}
                  alt={devHunt.alt}
                  width={220}
                  height={54}
                  className="h-9 w-auto"
                />
              </a>
            </span>
          </span>
        }
      />

      {/* The pilot, a week each. */}
      <div className="grid gap-6">
        {/* The pilot's label, and under it its promise as a heading
            (Giovani, 2026-10-29: the label alone said too little). */}
        <div className="grid justify-items-center gap-2 text-center">
          <p className="text-sm font-medium tracking-wide text-primary uppercase">
            {closing.pilot.title}
          </p>
          <h3 className="font-heading text-2xl font-semibold text-foreground">
            From your sign-in to proof, in four weeks
          </h3>
        </div>
        <PilotWithOwners />
      </div>
    </div>
  );
}

const bodies = [
  "Your provider, its groups as teams, and your own domain",
  "No rewrite: each built from its repository as it is",
  "With the AI agents and the tools they already use",
  "Cost, access and adoption, measured app by app",
];
// The pilot, as the site draws its roads (route-timeline.tsx; Giovani,
// 2026-10-29: the same animation as the site's): built once as it comes in
// view, the line drawn from the first week to the last, each week arriving
// as the line reaches it. "Week N" then its name, on two lines.
const pilotIcons: Record<string, React.ReactElement> = {
  "week-1": <KeyRound />,
  "week-2": <FolderInput />,
  "week-3": <Users />,
  "week-4": <BarChart3 />,
};
function PilotWithOwners() {
  return (
    <RouteTimeline
      steps={closing.pilot.weeks.map((w, i) => ({
        id: `week-${i + 1}`,
        title: `Week ${i + 1}\n${w.title}`,
        body: bodies[i],
      }))}
      icons={pilotIcons}
    />
  );
}
