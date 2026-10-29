"use client";

// Slide 8 told in steps (Giovani, 2026-10-28), a test beside the slide as it
// is (where-it-runs.tsx). The order follows the sentence: what does not
// change comes first, on its own, outside the choice: someone ships an app.
// Then where it runs is chosen, and the map tells it in beats, as slides 4 to
// 7 do: the app goes live, the orange thread runs from the sign-in through
// shpyrd to the data, the lines round who owns what draw in, and the four
// answers come in one by one, each lighting its part of the map. In your own
// cloud shpyrd's line stays and your company's grows round it: shpyrd moves
// inside.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  Building2,
  ChevronLeft,
  ChevronRight,
  Cloud,
  Database,
  KeyRound,
  ListChecks,
  Map as MapIcon,
  Pause,
  Play,
  Rocket,
  Server,
  Wallet,
  Waypoints,
  Wrench,
} from "lucide-react";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Button } from "@shpyrd/ui/components/button";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { cn } from "@shpyrd/ui/lib/cn";
import { delivery } from "@/content/tour-v3";
import { MiniShip } from "./mini-ship";
import { Intro, Label, panel, mouseOnly } from "./parts";

type Where = "cloud" | "own";
type Part = "build" | "platform" | "data" | "pay";

// The beats of an option, from its start: the home's story in small (the
// ask typed, sent, the app there, flying, live), the two threads, the lines,
// then the four answers. It moves on after HOLD.
// As slide 6 (Giovani, 2026-10-28), each box comes in at its turn, the
// orange thread growing ahead of it: the story, out of the cloud the thread
// to the map (5), the map (6), its two links (7, 8), its lines (9), on to the
// answers (10), the answers' box (11), then the answers one by one (12-15).
// The AI's window (Giovani, 2026-10-28): the ask typed and sent at the
// home's pace (1.7 s), then left a while to be read before the AI answers
// (3.7 s); flying (4.8 s), live (6.1 s). After the cloud the rest comes in
// quicker (Giovani, 2026-10-28: going back to an option to look again
// should not be a long wait): the thread on at 6.5 s, the map at 6.9, the
// answers' box at 8.7, its answers every 0.4 s.
const BEATS = [
  60, 1700, 3700, 4800, 6100, 6500, 6900, 7300, 7600, 7900, 8300, 8700, 9100,
  9500, 9900, 10300,
];
const HOLD = 15000;
const stepButton = "hover:border-primary/40 hover:text-primary";

const options: {
  id: Where;
  icon: React.ReactElement;
}[] = [
  {
    id: "cloud",
    // Our mark, in orange, for shpyrd cloud (Giovani, 2026-10-28).
    icon: <LogoMark className="size-6" />,
  },
  {
    id: "own",
    icon: <Building2 />,
  },
];

// What changes, as four questions, each lighting its part of the map.
const changes: {
  icon: React.ReactElement;
  question: string;
  part: Part;
  answer: Record<Where, string>;
}[] = [
  {
    icon: <Rocket />,
    question: "How you start",
    part: "build",
    answer: {
      cloud: "Today: connect your sign-in and ship the first app",
      own: "Install shpyrd in your own Kubernetes, on your account",
    },
  },
  {
    icon: <Wrench />,
    question: "Who runs the platform",
    part: "platform",
    answer: {
      cloud: "We do: updates and scaling included",
      own: "Your team does, with the code open to read",
    },
  },
  {
    icon: <Database />,
    question: "Where your data lives",
    part: "data",
    answer: {
      cloud: "With your apps on shpyrd; your systems over a private link",
      own: "Inside your network: it never leaves",
    },
  },
  {
    icon: <Wallet />,
    question: "How you pay",
    part: "pay",
    answer: {
      cloud: "For what runs, nothing for idle",
      own: "A license switches on the enterprise features",
    },
  },
];

function RouteBox({
  children,
  lit,
  size = "size-12",
}: {
  children: React.ReactNode;
  lit?: boolean;
  size?: string;
}) {
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-2xl ring-1 transition-[background-color,box-shadow,color] duration-normal ease-move [&_svg]:size-5",
        size,
        lit
          ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
          : "bg-background text-primary ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
      )}
    >
      {children}
    </span>
  );
}

function Box({
  icon,
  children,
  note,
  lit,
}: {
  icon: React.ReactElement;
  children: React.ReactNode;
  note: React.ReactNode;
  lit?: boolean;
}) {
  return (
    <div className="relative z-10 flex items-center gap-3 max-sm:flex-col max-sm:gap-2 max-sm:text-center">
      <RouteBox lit={lit}>{icon}</RouteBox>
      <div className="grid min-w-0">
        <span className="text-sm font-medium text-foreground">{children}</span>
        <span className="text-xs text-muted-foreground">{note}</span>
      </div>
    </div>
  );
}

// Slide 6's orange thread, drawn in at its beat.
function Thread({ on }: { on: boolean }) {
  return (
    <span aria-hidden="true" className="flex h-12 items-center">
      <span
        className="h-px w-full origin-left bg-primary transition-transform duration-900 ease-move"
        style={{ transform: `scaleX(${on ? 1 : 0})` }}
      />
    </span>
  );
}

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

// A box coming in at its turn, as slide 6's: from a little lower, clear.
const comeIn = (shown: boolean) =>
  cn(
    "transition-[opacity,translate] duration-1000 ease-move",
    shown ? "translate-y-0 opacity-100" : "translate-y-4 opacity-0",
  );

export function WhereItRunsStory() {
  const [at, setAt] = useState(0);
  const [moving, setMoving] = useState(true);
  const [hover, setHover] = useState<Part | null>(null);
  // How far into the option the story is, in ms. It always runs; the pause
  // only keeps the slide from moving on to the next option (Giovani,
  // 2026-10-28: not the story stopped half way).
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setMoving(false);
      setElapsed(HOLD);
    }
  }, []);
  useEffect(() => {
    let last = performance.now();
    const timer = window.setInterval(() => {
      const now = performance.now();
      const dt = now - last;
      last = now;
      setElapsed((e) => Math.min(e + dt, HOLD));
    }, 50);
    return () => window.clearInterval(timer);
  }, []);
  // At the end of an option, while the slide moves, on to the next.
  useEffect(() => {
    if (!moving || elapsed < HOLD) return;
    setAt((a) => (a + 1) % 2);
    setElapsed(0);
  }, [moving, elapsed]);
  // The beat the option has reached: -1 before its first.
  const beat = BEATS.reduce((b, t, i) => (elapsed >= t ? i : b), -1);
  // Chosen, an option starts from its beginning, and stays: it is there to
  // be looked at.
  const go = (i: number) => {
    setAt((i + 2) % 2);
    setElapsed(0);
    setMoving(false);
  };
  const where = options[at].id;
  const own = where === "own";
  // How many answers have come in.
  const told = beat - 12;
  // Only the pointer lights a part of the map (Giovani, 2026-10-28): lit by
  // the answers as they came in, it confused more than it showed.
  const focus: Part | null = hover;
  const lines = beat >= 9;

  // Where the boxes are, in the stage's own pixels (offsets, so the boxes'
  // coming in from lower does not move the thread).
  const stage = useRef<HTMLDivElement>(null);
  const [thread, setThread] = useState<string[] | null>(null);
  useLayoutEffect(() => {
    const el = stage.current;
    if (!el) return;
    const rel = (e: Element | null) => {
      if (!(e instanceof HTMLElement)) return null;
      let x = 0;
      let y = 0;
      let n: HTMLElement | null = e;
      while (n && n !== el) {
        x += n.offsetLeft;
        y += n.offsetTop;
        n = n.offsetParent as HTMLElement | null;
      }
      return { x, y, w: e.offsetWidth, h: e.offsetHeight };
    };
    const measure = () => {
      const cloud = rel(el.querySelector("[data-mini-cloud]"));
      const map = rel(el.querySelector("[data-box=map]"));
      const qa = rel(el.querySelector("[data-box=qa]"));
      if (!cloud || !map || !qa) return;
      const y = cloud.y + cloud.h * 0.6;
      const across = `M ${cloud.x + cloud.w - 6} ${y} L ${map.x} ${y}`;
      const R = 12;
      const mx = map.x + map.w / 2;
      const qx = qa.x + qa.w / 2;
      // Out of the map's foot, down and round a corner, across into the
      // answers' side when they reach lower than the map (each box as tall
      // as what it holds); else round under both, up into their foot.
      const yb = map.y + map.h + 24;
      const under =
        qa.y + qa.h > yb + 16
          ? `M ${mx} ${map.y + map.h} L ${mx} ${yb - R} Q ${mx} ${yb} ${mx + R} ${yb} L ${qa.x} ${yb}`
          : `M ${mx} ${map.y + map.h} L ${mx} ${yb - R} Q ${mx} ${yb} ${mx + R} ${yb} ` +
            `L ${qx - R} ${yb} Q ${qx} ${yb} ${qx} ${yb - R} L ${qx} ${qa.y + qa.h}`;
      setThread([across, under]);
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(el);
    const late = [300, 1200].map((t) => window.setTimeout(measure, t));
    return () => {
      seen.disconnect();
      late.forEach((t) => window.clearTimeout(t));
    };
  }, []);

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Cloud />}>{delivery.label}</Label>}
        heading={delivery.heading}
        lead={delivery.lead}
      />

      {/* 2. Where it runs, chosen on the design system's tabs, as slide 3
          switches its table, larger (Giovani, 2026-10-28). */}
      <div className="flex justify-center">
        <Tabs value={where} onValueChange={(v) => go(v === "own" ? 1 : 0)}>
          <TabsList
            aria-label="Where it runs"
            variant="pill"
            size="lg"
            // On a phone both fit: a little less room in each, smaller words.
            className="max-sm:[&_[role=tab]]:gap-1.5! max-sm:[&_[role=tab]]:px-2! max-sm:[&_[role=tab]]:text-sm!"
          >
            {options.map((o) => (
              <TabsTrigger
                {...mouseOnly}
                key={o.id}
                value={o.id}
                icon={
                  // The building orange while it is the one chosen, as our
                  // mark is orange (Giovani, 2026-10-28).
                  o.id === "own" ? (
                    <Building2
                      className={cn(
                        "transition-colors duration-normal",
                        own && "text-primary",
                      )}
                    />
                  ) : (
                    o.icon
                  )
                }
              >
                {delivery.options.find((d) => d.id === o.id)!.name}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      {/* Then three boxes side by side (Giovani, 2026-10-28): what does
          not change, the home's hero in small; where it runs, the map; what
          changes, the answers. */}
      <div
        ref={stage}
        className="relative grid items-start gap-6 pb-8 lg:grid-cols-[20rem_1fr_18rem]"
      >
        {/* The thread between the boxes, as slide 6's: out of the cloud's
            side across into the map; out of the map's foot, round two
            corners, up into the answers' foot. It grows ahead of each box. */}
        {thread && (
          <svg
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 z-30 size-full overflow-visible max-lg:hidden"
          >
            {thread.map((d, i) => (
              <path
                key={`${where}-${i}`}
                d={d}
                fill="none"
                pathLength={1}
                strokeDasharray="1 1"
                className="stroke-primary transition-[stroke-dashoffset] duration-900 ease-move"
                strokeWidth={1}
                style={{ strokeDashoffset: beat >= (i === 0 ? 5 : 10) ? 0 : 1 }}
              />
            ))}
          </svg>
        )}
        {/* 1. What does not change, first and on its own: someone ships,
          told as the home's hero tells it, in small (Giovani, 2026-10-28):
          out of a box, the AI's window floating; no words above it, the slide's
          lead says them (Giovani, 2026-10-28). */}
        <div className="grid min-w-0 content-start pt-5">
          <MiniShip key={at} step={beat} lit={focus === "build"} />
        </div>

        <div
          data-box="map"
          className={cn(
            panel,
            "grid content-start gap-3 p-5",
            comeIn(beat >= 6),
          )}
        >
          <BoxHeading icon={<MapIcon />}>
            Where it runs, and who looks after it
          </BoxHeading>
          <div className="relative mt-3 px-4 py-8">
            {/* Your company's line: round your side on shpyrd cloud, round
                everything, shpyrd too, in your own. */}
            <div
              aria-hidden="true"
              className={cn(
                "absolute inset-y-0 left-0 rounded-2xl border border-dashed border-info/60 transition-[width,background-color,opacity] duration-slow ease-move",
                own ? "w-full bg-info/5" : "w-[calc(50%-0.75rem)]",
                lines ? "opacity-100" : "opacity-0",
              )}
            />
            {/* shpyrd's line: it stays, and in your own cloud it is held
                inside your company's. */}
            <div
              aria-hidden="true"
              className={cn(
                "absolute w-[calc(50%-0.75rem)] rounded-2xl border border-dashed border-primary/60 bg-primary/5 transition-[top,bottom,right,width,opacity] duration-slow ease-move",
                own
                  ? "top-6 right-3 bottom-3 w-[calc(50%-1.25rem)]"
                  : "top-0 right-0 bottom-0",
                lines ? "opacity-100" : "opacity-0",
              )}
            />
            <p
              key={`left-${where}`}
              className={cn(
                "absolute -top-2.5 left-4 bg-card px-1.5 text-xs font-medium text-info transition-opacity duration-normal",
                lines ? "opacity-100" : "opacity-0",
              )}
            >
              {own ? delivery.ownZone : delivery.company}
            </p>
            <p
              className={cn(
                "absolute right-6 px-1.5 text-xs font-medium text-primary transition-[top,opacity] duration-slow ease-move",
                own ? "top-1.5" : "-top-2.5 bg-card",
                lines ? "opacity-100" : "opacity-0",
              )}
            >
              {own ? "shpyrd, in your Kubernetes" : delivery.options[0].name}
            </p>

            <div className="grid grid-cols-[1fr_2.5rem_1fr] items-center gap-x-3 gap-y-6 max-sm:grid-cols-[1fr_1rem_1fr] max-sm:items-start max-sm:gap-x-1">
              <Box icon={<KeyRound />} note="Entra · Google · Okta">
                Your sign-in
              </Box>
              <Thread on={beat >= 7} />
              <Box
                icon={<Waypoints />}
                lit={focus === "platform" || focus === "pay"}
                note={own ? "Run by your team" : "Run and updated by us"}
              >
                {delivery.platformBox}
              </Box>
              <Box icon={<Server />} note="What your apps reach">
                {delivery.systems}
              </Box>
              <Thread on={beat >= 8} />
              <Box icon={<Database />} lit={focus === "data"} note="Your data">
                {delivery.databases}
              </Box>
            </div>
          </div>
          <p
            className={cn(
              "text-center text-xs text-muted-foreground transition-opacity duration-normal",
              lines ? "opacity-100" : "opacity-0",
            )}
          >
            {own ? delivery.ownLink : delivery.link}
          </p>
        </div>

        <div
          data-box="qa"
          className={cn(
            panel,
            "grid content-start gap-2 p-5",
            comeIn(beat >= 11),
          )}
        >
          <BoxHeading icon={<ListChecks />}>
            What changes on {own ? "your own cloud" : "shpyrd cloud"}
          </BoxHeading>
          <div className="-mx-2 grid gap-1">
            {changes.map((c, i) => {
              const shown = told >= i;
              return (
                <div
                  key={c.question}
                  onMouseEnter={() => shown && setHover(c.part)}
                  onMouseLeave={() => setHover(null)}
                  className={cn(
                    "flex items-center gap-3 rounded-xl px-2 py-2.5 transition-[background-color,opacity,translate] duration-normal ease-move",
                    shown ? "opacity-100" : "translate-y-2 opacity-0",
                    focus === c.part && "bg-primary/5",
                  )}
                >
                  <RouteBox size="size-11" lit={focus === c.part}>
                    {c.icon}
                  </RouteBox>
                  <div className="grid gap-0.5">
                    <span className="text-xs text-muted-foreground">
                      {c.question}
                    </span>
                    <span className="text-sm font-medium text-foreground">
                      {c.answer[where]}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <div className="-mt-2 flex items-center justify-center gap-2">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => go(at - 1)}
          aria-label="Previous option"
        >
          <ChevronLeft />
        </Button>
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
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => go(at + 1)}
          aria-label="Next option"
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
