"use client";

// A proposal for slide 9 (Giovani, 2026-10-28), beside the slide as it is
// (governance.tsx). What it says: builders are free to ship, and IT sees all
// of it. So the three parts tell one story: each line of the audit trail
// changes the list and the figures as it is written. A deploy lights its app
// and its version; an app that sleeps turns asleep and the count of those
// asleep goes up; someone turned away lights the app's "who can open"; and,
// for the freedom, someone ships a new app and it joins the list by itself,
// no ticket. Then the day starts again.
import { useEffect, useState } from "react";
import {
  Ban,
  KeyRound,
  ShieldCheck,
  Wallet,
  LayoutGrid,
  ListChecks,
  Moon,
  Pause,
  Play,
  Rocket,
  ScrollText,
  Share2,
  Sun,
  Undo2,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Badge } from "@shpyrd/ui/components/badge";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Button } from "@shpyrd/ui/components/button";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { AppIcon, appIcons, symbolInk } from "@shpyrd/ui/components/app-icons";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { governance } from "@/content/tour-v3";
import { Intro, Label, panel, rise, mouseOnly } from "./parts";

type App = {
  app: string;
  icon: string;
  colour: string;
  team: string;
  owner: string;
  access: string;
  version: number;
  cost: string;
  awake: boolean;
};

// The list at the start of the day. The day opens with a new app, which
// takes the top and sends the last one (inventory) off the foot; the others
// are the ones the day's events are about.
const start: App[] = [
  {
    app: "contracts",
    icon: "file-text",
    colour: "teal",
    team: "Legal",
    owner: "Julia Reis",
    access: "Legal team",
    version: 3,
    cost: "$0.60",
    awake: false,
  },
  {
    app: "expenses",
    icon: "receipt",
    colour: "green",
    team: "Finance",
    owner: "Marina Costa",
    access: "Finance team",
    version: 13,
    cost: "$4.10",
    awake: true,
  },
  {
    app: "pipeline",
    icon: "chart-line",
    colour: "blue",
    team: "Sales",
    owner: "Diego Alves",
    access: "Sales team",
    version: 22,
    cost: "$6.80",
    awake: false,
  },
  {
    app: "campaigns",
    icon: "megaphone",
    colour: "pink",
    team: "Marketing",
    owner: "Bia Nunes",
    access: "Bia only",
    version: 8,
    cost: "$2.40",
    awake: true,
  },
  {
    app: "inventory",
    icon: "package",
    colour: "amber",
    team: "Ops",
    owner: "Rafa Lima",
    access: "Ops team",
    version: 7,
    cost: "$1.20",
    awake: false,
  },
];
// The figures beside the apps shown: the company has 47 in all.
const APPS = 47;
const ASLEEP = 31;
// What an app costs a month on average, as the list's own costs go
// (Giovani, 2026-10-29: the total has to add up with the list), and kept on
// as slide 7 counts it ($18 the app, $22 its database).
const OURS = 2.8;
const ALWAYS = 40;
// The icons and colours the company's other apps take, from the set.
const iconNames = Object.keys(appIcons);
const tones = [
  "green",
  "blue",
  "violet",
  "pink",
  "amber",
  "teal",
  "indigo",
  "cyan",
  "red",
];
// The company's 47 apps by team, for the "teams" look.
const teams = [
  { team: "Sales", apps: 11 },
  { team: "Finance", apps: 10 },
  { team: "Ops", apps: 8 },
  { team: "People", apps: 7 },
  { team: "Marketing", apps: 7 },
  { team: "Legal", apps: 4 },
];
const TEAM_MAX = 12;
const money = (n: number) => `$${Math.round(n).toLocaleString("en")}`;

type Part = "row" | "version" | "access" | "state" | "secret";
// The day's events, one by one: what the trail writes, its mark, and what it
// does to the list. The freedom first (Giovani, 2026-10-29): someone ships
// a new app, no ticket; then what IT sees of the day.
const events: {
  message: string;
  icon: React.ReactElement;
  type: "neutral" | "primary" | "success" | "info" | "warning" | "error";
  // Who did it, for the trail's avatar; none for what the platform did.
  who?: string;
  app: string;
  part: Part;
  change: (a: App[]) => App[];
}[] = [
  {
    message: "Lucas Prado shipped a new app: hiring",
    who: "Lucas Prado",
    icon: <Rocket />,
    type: "primary",
    app: "hiring",
    part: "row",
    change: (a) => [
      {
        app: "hiring",
        icon: "users",
        colour: "indigo",
        team: "People",
        owner: "Lucas Prado",
        access: "People team",
        version: 1,
        cost: "$0.00",
        awake: true,
      },
      ...a,
    ],
  },
  {
    message: "Marina Costa deployed expenses v14",
    who: "Marina Costa",
    icon: <Rocket />,
    type: "primary",
    app: "expenses",
    part: "version",
    change: (a) =>
      a.map((r) => (r.app === "expenses" ? { ...r, version: 14 } : r)),
  },
  {
    message: "Diego Alves was turned away at contracts",
    who: "Diego Alves",
    icon: <Ban />,
    type: "error",
    app: "contracts",
    part: "access",
    change: (a) => a,
  },
  {
    message: "Bia Nunes shared campaigns with the Marketing team",
    who: "Bia Nunes",
    icon: <Share2 />,
    type: "info",
    app: "campaigns",
    part: "access",
    change: (a) =>
      a.map((r) =>
        r.app === "campaigns" ? { ...r, access: "Marketing team" } : r,
      ),
  },
  {
    message: "pipeline woke up for Diego Alves",
    icon: <Sun />,
    type: "success",
    app: "pipeline",
    part: "state",
    change: (a) =>
      a.map((r) => (r.app === "pipeline" ? { ...r, awake: true } : r)),
  },
  {
    message: "campaigns fell asleep after 15 min with nobody",
    icon: <Moon />,
    type: "neutral",
    app: "campaigns",
    part: "state",
    change: (a) =>
      a.map((r) => (r.app === "campaigns" ? { ...r, awake: false } : r)),
  },
  {
    message: "Marina Costa rolled back expenses to v13",
    who: "Marina Costa",
    icon: <Undo2 />,
    type: "warning",
    app: "expenses",
    part: "version",
    change: (a) =>
      a.map((r) => (r.app === "expenses" ? { ...r, version: 13 } : r)),
  },
  {
    message: "Julia Reis set a new secret on contracts",
    who: "Julia Reis",
    icon: <KeyRound />,
    type: "neutral",
    app: "contracts",
    part: "secret",
    change: (a) => a,
  },
];
// A line every few seconds; after the last, a while before the day again.
// The list shows five apps (Giovani, 2026-10-29: the slide shorter): the
// new one on top, the last leaves at the foot, so the box keeps its height.
const ROWS = 5;
const EVERY = 2600;
const REST = 4000;
// Which box of the figures an event is about, lit as its trail line is
// pointed at.
const boxOf: Record<Part, "apps" | "safe" | null> = {
  row: "apps",
  state: "apps",
  access: "safe",
  secret: "safe",
  version: null,
};
const stepButton = "hover:border-primary/40 hover:text-primary";

// The trail's clock: 14:02:10, then a few seconds between lines.
const at = (i: number) => {
  const s = 14 * 3600 + 2 * 60 + 10 + i * 7;
  return [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60]
    .map((n) => String(n).padStart(2, "0"))
    .join(":");
};

// A heading as slides 6 and 8 have them: small, muted, an icon in orange.
function BoxHeading({
  icon,
  children,
}: {
  icon: React.ReactElement;
  children: React.ReactNode;
}) {
  return (
    <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground [&_svg]:size-3.5 [&_svg]:text-primary">
      {icon}
      {children}
    </p>
  );
}

// The boxes come in one after another as the slide opens, 120 ms apart,
// as slide 6's (Giovani, 2026-10-29).
// Under the pointer, as the design system has it (Giovani, 2026-10-29): a
// row of the list the table's own grey, a box an orange edge only; the
// trail lights only their small parts.
// The words as on slides 7 and 8: captions 12 px muted, values 14 px in the
// foreground; the four figures of the boxes one size, 30 px semibold, each
// with its 12 px caption (Giovani, 2026-10-29). The cards rest on the slide: no shadow.
export function GovernanceStory({
  apps: look = "teams",
}: {
  // How the apps' box draws them (Giovani, 2026-10-29: the blocks mean use
  // and cost on the home and on slide 7): by team, chosen; the blocks, their
  // own icons and one bar awake against asleep kept as tests.
  apps?: "blocks" | "mosaic" | "bar" | "teams";
} = {}) {
  const mosaic = look === "mosaic";
  // How many of the day's events have happened.
  const [done, setDone] = useState(0);
  const [moving, setMoving] = useState(true);
  // The trail line under the pointer: its app's row and its box light, as
  // slide 8's answers light the map (Giovani, 2026-10-29).
  const [pointed, setPointed] = useState<(typeof events)[number] | null>(null);
  // And the other way: the row under the pointer lights its app's lines in
  // the trail, and its block.
  const [rowApp, setRowApp] = useState<string | null>(null);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setMoving(false);
      setDone(events.length);
    }
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(
      () => setDone((d) => (d >= events.length ? 0 : d + 1)),
      done >= events.length ? REST : done === 0 ? 900 : EVERY,
    );
    return () => window.clearTimeout(timer);
  }, [moving, done]);

  const list = events.slice(0, done).reduce((a, e) => e.change(a), start);
  const last = done > 0 ? events[done - 1] : null;
  const asleep =
    ASLEEP +
    list.filter((r) => !r.awake).length -
    start.filter((r) => !r.awake).length;
  const apps = APPS + list.length - start.length;
  // What the newest line lit, in the list and in the figures.
  const lit = (app: string, part: Part) =>
    last?.app === app && last.part === part;
  // The trail, newest on top.
  const trail = events
    .slice(0, done)
    .map((e, i) => ({ ...e, i, time: at(i) }))
    .reverse();

  // The company's apps as blocks, the six of the list first: awake orange,
  // asleep grey; the others as the day finds them.
  const others = APPS - start.length;
  const othersAsleep = ASLEEP - start.filter((r) => !r.awake).length;
  const blocks = [
    ...list.map((r) => ({
      id: r.app,
      awake: r.awake,
      icon: r.icon,
      colour: r.colour,
    })),
    ...Array.from({ length: others }, (_, i) => ({
      id: `other-${i}`,
      awake: (i * 7) % others >= othersAsleep,
      // The company's other apps, each with an icon and a colour of the set.
      icon: iconNames[(i * 5) % iconNames.length],
      colour: tones[(i * 3) % tones.length],
    })),
  ];
  const turnedAway = events
    .slice(0, done)
    .filter((e) => e.type === "error").length;
  // Whether the day's new secret has been set: shpyrd then keeps three.
  const secretSet = events.slice(0, done).some((e) => e.part === "secret");

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<LayoutGrid />}>{governance.label}</Label>}
        heading={governance.heading}
        lead={governance.lead}
      />

      <div className="grid gap-4">
        {/* The figures, each shown rather than said (Giovani, 2026-10-29). */}
        <div className="grid gap-4 lg:grid-cols-[1fr_21rem_21rem]">
          {/* Every app as a block: awake orange, asleep grey; a new one
              joins at the end. As the home's cost film draws its blocks;
              sixteen to a row, as wide as the box, so three rows fill it
              (Giovani, 2026-10-29: no white under them). */}
          <div
            style={{ animationDelay: "150ms" }}
            className={cn(
              panel,
              rise,
              "grid content-start gap-2.5 p-4 transition-[border-color,background-color] duration-normal hover:border-primary/40",
              pointed && boxOf[pointed.part] === "apps" && "border-primary/40",
            )}
          >
            <BoxHeading icon={<LayoutGrid />}>Apps in one list</BoxHeading>
            <div className="flex items-end justify-between gap-3">
              <p className="flex items-baseline gap-2">
                <span className="font-heading text-3xl font-semibold text-foreground tabular-nums">
                  {apps}
                </span>
                <span className="text-xs text-muted-foreground">apps</span>
              </p>
              <ul className="flex gap-3 text-xs text-muted-foreground tabular-nums">
                <li className="flex items-center gap-1.5">
                  {(look === "blocks" || look === "bar") && (
                    <span className="size-2 rounded-[2px] bg-primary" />
                  )}
                  {apps - asleep} awake
                </li>
                <li className="flex items-center gap-1.5">
                  {(look === "blocks" || look === "bar") && (
                    <span className="size-2 rounded-[2px] bg-foreground/15" />
                  )}
                  {asleep} asleep
                </li>
              </ul>
            </div>
            {look === "bar" ? (
              // One bar: the apps awake, orange, against those asleep, grey;
              // a new app widens it.
              <div aria-hidden="true" className="grid gap-2">
                <div className="flex h-3 gap-0.5">
                  <span
                    className="rounded-[3px] bg-primary transition-[flex-grow] duration-slow ease-move"
                    style={{ flexGrow: apps - asleep }}
                  />
                  <span
                    className="rounded-[3px] bg-foreground/10 transition-[flex-grow] duration-slow ease-move"
                    style={{ flexGrow: asleep }}
                  />
                </div>
                <p className="flex justify-between text-xs text-muted-foreground tabular-nums">
                  <span>
                    {Math.round(((apps - asleep) / apps) * 100)}% awake now
                  </span>
                  <span>
                    {Math.round((asleep / apps) * 100)}% asleep, costing nothing
                  </span>
                </p>
              </div>
            ) : look === "teams" ? (
              // The apps by team: a bar each, as long as its count; the new
              // app adds to its team, which lights.
              <ul aria-hidden="true" className="grid gap-1">
                {teams.map((t) => {
                  const n =
                    t.apps +
                    list.filter(
                      (r) =>
                        r.team === t.team &&
                        !start.some((x) => x.app === r.app),
                    ).length;
                  const grew = last?.part === "row" && t.team === "People";
                  return (
                    <li
                      key={t.team}
                      className="grid grid-cols-[5.5rem_1fr_1.5rem] items-center gap-2 text-xs"
                    >
                      <span className="truncate text-muted-foreground">
                        {t.team}
                      </span>
                      {/* The design system's bar, as the costs' beside it
                          (Giovani, 2026-10-29). */}
                      <ProgressBar
                        value={(n / TEAM_MAX) * 100}
                        tone="orange"
                        size="sm"
                      />
                      <span
                        className={cn(
                          "text-right tabular-nums",
                          grew ? "font-medium text-primary" : "text-foreground",
                        )}
                      >
                        {n}
                      </span>
                    </li>
                  );
                })}
              </ul>
            ) : mosaic ? (
              // Every app its own icon, as in the list and the workspace:
              // awake in its colour, asleep grey.
              <div aria-hidden="true" className="grid grid-cols-16 gap-1">
                {blocks.map((b) => (
                  <span
                    key={b.id}
                    style={b.awake ? symbolInk(b.colour) : undefined}
                    className={cn(
                      "grid aspect-square place-items-center rounded-[4px] transition-colors duration-slow",
                      b.awake
                        ? "bg-(--ink)/15 text-(--ink)"
                        : "bg-foreground/[0.05] text-foreground/25",
                      (rowApp ?? (pointed ?? last)?.app) === b.id &&
                        "ring-2 ring-primary/40 ring-offset-1 ring-offset-background",
                      b.id === "hiring" &&
                        "animate-in zoom-in-50 fade-in duration-slow",
                    )}
                  >
                    <AppIcon name={b.icon} className="size-3" />
                  </span>
                ))}
              </div>
            ) : (
              <div aria-hidden="true" className="grid grid-cols-16 gap-[3px]">
                {blocks.map((b) => (
                  <span
                    key={b.id}
                    className={cn(
                      "aspect-square rounded-[3px] transition-colors duration-slow",
                      b.awake ? "bg-primary" : "bg-foreground/10",
                      (rowApp ?? (pointed ?? last)?.app) === b.id &&
                        "ring-2 ring-primary/40 ring-offset-1 ring-offset-background",
                      b.id === "hiring" &&
                        "animate-in zoom-in-50 fade-in duration-slow",
                    )}
                  />
                ))}
              </div>
            )}
          </div>

          {/* Safe by default, in one box (Giovani, 2026-10-29), its two
              figures plain as the other boxes' (no ring, no lock): every app
              behind your sign-in, who was turned away today; no secret in
              the code, the keys shpyrd keeps instead. */}
          <div
            style={{ animationDelay: "270ms" }}
            className={cn(
              panel,
              rise,
              "grid grid-rows-[auto_1fr_1fr] gap-2.5 p-4 transition-[border-color,background-color] duration-normal hover:border-primary/40",
              pointed && boxOf[pointed.part] === "safe" && "border-primary/40",
            )}
          >
            <BoxHeading icon={<ShieldCheck />}>Safe by default</BoxHeading>
            <div className="flex items-center gap-3">
              <p className="grid">
                <span className="font-heading text-3xl font-semibold text-foreground tabular-nums">
                  100%
                </span>
                <span className="text-xs whitespace-nowrap text-muted-foreground">
                  behind your sign-in
                </span>
              </p>
              <span
                className={cn(
                  "ml-auto flex items-center gap-1.5 text-xs whitespace-nowrap tabular-nums transition-colors duration-normal [&_svg]:size-3.5",
                  last?.type === "error"
                    ? "text-destructive"
                    : "text-muted-foreground",
                )}
              >
                <Ban />
                {turnedAway} turned away today
              </span>
            </div>
            <div className="flex items-center gap-3 border-t border-border pt-3">
              <p className="grid">
                <span className="font-heading text-3xl font-semibold text-foreground tabular-nums">
                  0
                </span>
                <span className="text-xs whitespace-nowrap text-muted-foreground">
                  secrets in the code
                </span>
              </p>
              <span
                className={cn(
                  "ml-auto flex items-center gap-1.5 text-xs whitespace-nowrap tabular-nums transition-colors duration-normal [&_svg]:size-3.5",
                  last?.part === "secret"
                    ? "text-primary"
                    : "text-muted-foreground",
                )}
              >
                <KeyRound />
                {secretSet ? 3 : 2} kept by shpyrd
              </span>
            </div>
          </div>

          {/* What it all costs a month, as slide 7 counts it: on shpyrd,
              and what it would be kept on (Giovani, 2026-10-29). */}
          <div
            style={{ animationDelay: "390ms" }}
            className={cn(
              panel,
              rise,
              "grid grid-rows-[auto_auto_1fr_auto] gap-2.5 p-4 transition-[border-color,background-color] duration-normal hover:border-primary/40",
            )}
          >
            <BoxHeading icon={<Wallet />}>What it all costs</BoxHeading>
            <p className="flex items-baseline gap-2">
              <span className="font-heading text-3xl font-semibold text-foreground tabular-nums">
                {money(apps * OURS)}
              </span>
              <span className="text-xs text-muted-foreground tabular-nums">
                a month, {apps} apps
              </span>
            </p>
            <div className="grid content-center gap-2">
              <div className="grid gap-1">
                <p className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">On shpyrd</span>
                  <span className="font-medium text-foreground tabular-nums">
                    {money(apps * OURS)}
                  </span>
                </p>
                <ProgressBar
                  value={(OURS / ALWAYS) * 100}
                  tone="success"
                  size="sm"
                />
              </div>
              <div className="grid gap-1">
                <p className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">Kept on</span>
                  <span className="font-medium text-foreground tabular-nums">
                    {money(apps * ALWAYS)}
                  </span>
                </p>
                <ProgressBar value={100} tone="error" size="sm" />
              </div>
            </div>
            {/* What that leaves, as slide 7's Stat says it. */}
            <p className="flex items-center justify-between border-t border-border pt-3 text-xs">
              <span className="text-muted-foreground">You save</span>
              <span className="font-medium text-success tabular-nums">
                {money(apps * (ALWAYS - OURS))} a month ·{" "}
                {Math.round((1 - OURS / ALWAYS) * 100)}%
              </span>
            </p>
          </div>
        </div>

        <div className="grid items-stretch gap-4 lg:grid-cols-[1.5fr_1fr]">
          <div
            className={cn(panel, rise, "grid gap-2 p-4")}
            style={{ animationDelay: "510ms" }}
          >
            <BoxHeading icon={<ListChecks />}>
              Every app, in one list
            </BoxHeading>
            <div className="-mx-2 overflow-x-auto">
              {/* Its columns fixed, so a wider badge or a new row does not
                  move them as the trail writes (Giovani, 2026-10-29). */}
              {/* On a phone the app, its cost and its state; who owns it and
                  who can open it from a tablet up. */}
              <Table className="table-fixed max-sm:[&_tr>*:nth-child(2)]:hidden max-sm:[&_tr>*:nth-child(3)]:hidden">
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[29%] max-sm:w-[50%]">App</TableHead>
                    <TableHead className="w-[21%]">Owner</TableHead>
                    <TableHead className="w-[20%]">Who can open</TableHead>
                    <TableHead className="w-[15%] max-sm:w-[22%]">
                      <span className="max-sm:hidden">Cost / month</span>
                      <span className="sm:hidden">Cost</span>
                    </TableHead>
                    <TableHead className="w-[15%] max-sm:w-[28%]">State</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {list.slice(0, ROWS).map((r) => (
                    <TableRow
                      key={r.app}
                      onMouseEnter={() => setRowApp(r.app)}
                      onMouseLeave={() => setRowApp(null)}
                      className={cn(
                        "transition-colors duration-normal",
                        pointed?.app === r.app && "bg-muted/50",
                        lit(r.app, "row") &&
                          "animate-in fade-in slide-in-from-top-2 duration-slow",
                      )}
                    >
                      <TableCell>
                        <span className="flex items-center gap-2.5">
                          {/* The app's own icon and colour, as its card in
                              the workspace has them. */}
                          <span
                            className="grid size-7 shrink-0 place-items-center rounded-lg bg-(--ink)/12 text-(--ink)"
                            style={symbolInk(r.colour)}
                          >
                            <AppIcon name={r.icon} className="size-3.5" />
                          </span>
                          <span className="grid">
                            <span className="font-medium text-foreground">
                              {r.app}{" "}
                              <span className="text-xs font-normal text-muted-foreground max-sm:hidden">
                                {r.team}
                              </span>
                            </span>
                            <span
                              className={cn(
                                "text-xs tabular-nums transition-colors duration-normal",
                                lit(r.app, "version")
                                  ? "font-medium text-primary"
                                  : "text-muted-foreground",
                              )}
                            >
                              v{r.version}
                              {/* The freedom, said on the row: shipped by
                                  whoever built it, without asking IT. */}
                              {r.app === "hiring" && (
                                <span className="font-medium text-primary">
                                  {" "}
                                  · New, no ticket
                                </span>
                              )}
                            </span>
                          </span>
                        </span>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        <span className="flex items-center gap-2">
                          <Avatar alt={r.owner} size={20} />
                          {r.owner}
                        </span>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant="secondary"
                          className={cn(
                            "transition-colors duration-normal",
                            lit(r.app, "access") &&
                              (last?.type === "error"
                                ? "bg-destructive/10 text-destructive"
                                : "bg-primary/10 text-primary"),
                          )}
                        >
                          {r.access}
                        </Badge>
                      </TableCell>
                      <TableCell className="tabular-nums">{r.cost}</TableCell>
                      <TableCell>
                        <StatusBadge type={r.awake ? "success" : "neutral"}>
                          {r.awake ? "Awake" : "Asleep"}
                        </StatusBadge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <p className="px-0.5 text-xs text-muted-foreground tabular-nums">
              Showing {ROWS} of {apps} apps
            </p>
          </div>

          <div
            className={cn(panel, rise, "flex flex-col gap-2 p-4")}
            style={{ animationDelay: "630ms" }}
          >
            <BoxHeading icon={<ScrollText />}>
              Audit trail, every change
            </BoxHeading>
            {/* Newest on top: a new line opens its room, pushing the others
                down, and slides in. */}
            <Timeline
              clip="end"
              // As tall as the list beside it; under it, on a phone, its own
              // room for a few lines.
              className="min-h-0 flex-1 basis-0 overflow-hidden [mask-image:linear-gradient(to_bottom,black_80%,transparent)] max-lg:min-h-72"
            >
              {trail.map((l) => (
                <TimelineItem
                  key={l.i}
                  onMouseEnter={() => setPointed(l)}
                  onMouseLeave={() => setPointed(null)}
                  condensed
                  icon={l.icon}
                  type={l.type}
                  className="group/line cursor-default [&_[data-slot=timeline-badge]]:text-primary animate-[tour-open_var(--transition-duration-slow)_var(--ease-move)_both] [overflow-x:visible] [overflow-y:clip]"
                >
                  <span
                    className={cn(
                      // Under the pointer the line reads as the list's row
                      // does (Giovani, 2026-10-29): the table's own grey
                      // behind it, its words in the ink.
                      "-mx-2 grid flex-1 gap-0.5 rounded-lg px-2 py-1 text-sm transition-colors duration-fast animate-in fade-in slide-in-from-left-2 fill-mode-both delay-150 duration-normal ease-enter group-hover/line:bg-muted/50 group-hover/line:text-foreground",
                      l.i === done - 1 || rowApp === l.app
                        ? "text-foreground"
                        : "text-muted-foreground",
                      rowApp === l.app && "bg-muted/50",
                    )}
                  >
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {l.time}
                    </span>
                    <span className="flex items-start gap-2">
                      {l.who && (
                        <Avatar
                          alt={l.who}
                          size={20}
                          className="mt-px shrink-0"
                        />
                      )}
                      {l.message}
                    </span>
                  </span>
                </TimelineItem>
              ))}
            </Timeline>
          </div>
        </div>
      </div>

      {/* As the other slides: play or pause the day. */}
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
