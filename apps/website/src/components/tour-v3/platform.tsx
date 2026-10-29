"use client";

// Slide 4: the layers as a journey (Giovani, 2026-10-28, from the proposal
// at /proposals/platform-timeline),
// left to right, the way a request makes it. From the person signing in to
// the app's data, a track with a stop for each layer; the request, an orange
// dot, travels it and halts at each stop, the track lit behind it. Each stop
// is told as the question it answers ("Who are you?"), so it reads as a
// story, not a list of terms; the card under the track says what shpyrd does
// there and what the builder doesn't have to. Under the track, a second
// line: logs, metrics and the audit trail, along the whole way. Left alone it
// moves on every few seconds; choosing a stop, or the arrows, holds it.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  Activity,
  AppWindow as AppWindowIcon,
  Building2,
  ChevronLeft,
  ChevronRight,
  Database,
  DoorOpen,
  Layers,
  Network,
  Pause,
  Play,
  Server,
  UsersRound,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { Button } from "@shpyrd/ui/components/button";
import { ChecklistItems } from "@shpyrd/ui/components/checklist";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { platform } from "@/content/tour-v3";
import { Intro, Label, panel, rise, mouseOnly } from "./parts";

const icons: Record<string, React.ReactElement> = {
  door: <DoorOpen />,
  roles: <UsersRound />,
  network: <Network />,
  runtime: <Server />,
  data: <Database />,
};

// The question each stop answers, and what the builder is spared there.
// What happens there, in two short lines for anyone (Giovani, 2026-10-28),
// as the start's and the end's cards say theirs.
const told: Record<string, { question: string; says: string; spared: string }> =
  {
    door: {
      question: "Who are you?",
      says: "Everyone signs in with their work account first. Strangers never reach the app.",
      spared: "No sign-in to build",
    },
    roles: {
      question: "May you open this app?",
      says: "The app picks who may open it: some people, a team, or the whole company.",
      spared: "No permissions to code",
    },
    network: {
      question: "Is this the only way in?",
      says: "Each app sits in its own closed space. The sign-in is the only door to reach it.",
      spared: "No firewall to set up",
    },
    runtime: {
      question: "Where does it run?",
      says: "On whatever language your AI picked, built as it is, asleep when nobody uses it.",
      spared: "No server to look after",
    },
    data: {
      question: "Where is its data kept?",
      says: "In a database of its own, backed up for you, paused when nobody is connected.",
      spared: "No database to run or back up",
    },
  };

const HOLD = 4500;

// The controls under the card: the outline icon buttons, their hover made
// plain (Giovani, 2026-10-28): the edge and the icon turn orange.
const stepButton = "hover:border-primary/40 hover:text-primary";

const stops = platform.layers.length;

// Where the request is: -1 signing in, 0 to 4 the stops, 5 in the app.
const START = -1;
const END = stops;
const EMAIL = platform.who;

export function Platform() {
  const [at, setAt] = useState(START);
  const [moving, setMoving] = useState(true);
  // The sign-in typed out, letter by letter, then its button pressed.
  const [typed, setTyped] = useState(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setMoving(false);
      setTyped(EMAIL.length);
    }
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(
      () => setAt((a) => (a >= END ? START : a + 1)),
      HOLD,
    );
    return () => window.clearTimeout(timer);
  }, [moving, at]);
  useEffect(() => {
    if (at !== START) return setTyped(EMAIL.length);
    setTyped(0);
    const timer = window.setInterval(
      () => setTyped((n) => (n >= EMAIL.length ? n : n + 1)),
      70,
    );
    return () => window.clearInterval(timer);
  }, [at]);
  const go = (i: number) => {
    setAt(i < START ? END : i > END ? START : i);
    setMoving(false);
  };
  // Where the request is on the track: the stops sit at the middle of five
  // equal columns; before the first it waits at the start, after the last it
  // has reached the end.
  const head = at === START ? 0 : at === END ? 100 : ((at + 0.5) / stops) * 100;
  const signedIn = at !== START || typed >= EMAIL.length;
  // The thread's path, measured in the journey's box.
  const way = useRef<HTMLDivElement>(null);
  const [thread, setThread] = useState<string | null>(null);
  useLayoutEffect(() => {
    const box = way.current;
    if (!box) return;
    const measure = () => {
      const o = box.getBoundingClientRect();
      const from = box
        .querySelector(`[data-place="${at}"]`)
        ?.getBoundingClientRect();
      const pill = box.querySelector("[data-logs]")?.getBoundingClientRect();
      const card = box.querySelector("[data-cards]")?.getBoundingClientRect();
      if (!from || !pill || !card) return;
      const x0 = from.left + from.width / 2 - o.left;
      const y0 = from.bottom - o.top + 10;
      const x1 = pill.left + pill.width / 2 - o.left;
      const y1 = pill.top + pill.height / 2 - o.top;
      const y2 = card.top - o.top;
      // Down from the stop, a rounded corner, across to the pill's middle,
      // another rounded corner, down through the pill to the card (Giovani,
      // 2026-10-28). The same commands whatever the stop, so it can ease.
      // The run across halfway between the journey and the pill's top
      // (Giovani, 2026-10-28), the same room above it as under it.
      // Measured from the journey's foot, not the stop's, so the run across
      // keeps one height whichever stop (their questions are one or two
      // lines; Giovani, 2026-10-28).
      const row =
        box.querySelector("[data-journey]")?.getBoundingClientRect() ?? from;
      const mid = (row.bottom + pill.top) / 2 - o.top;
      const dir = Math.sign(x1 - x0);
      const r = Math.min(16, Math.abs(x1 - x0) / 2, mid - y0);
      setThread(
        `M ${x0} ${y0} L ${x0} ${mid - r} Q ${x0} ${mid} ${x0 + dir * r} ${mid} ` +
          `L ${x1 - dir * r} ${mid} Q ${x1} ${mid} ${x1} ${mid + r} L ${x1} ${y1} L ${x1} ${y2}`,
      );
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(box);
    return () => seen.disconnect();
  }, [at]);

  // A card for each place on the way: the start, a stop, or the end.
  const card = (pos: number) => {
    const l = pos >= 0 && pos < stops ? platform.layers[pos] : null;
    return l ? (
      <div
        key={l.id}
        className={cn(
          panel,
          "grid h-full gap-6 p-8 md:grid-cols-[1fr_42rem] md:gap-12",
        )}
      >
        <div className={cn("grid content-start gap-3", rise)}>
          <p className="text-xs text-primary tabular-nums">
            Step {pos + 1} of {stops} · {l.name}
          </p>
          <h2 className="flex items-center gap-3 font-heading text-2xl font-semibold [&_svg]:size-6 [&_svg]:text-primary">
            {icons[l.id]}
            {told[l.id].question}
          </h2>
          <p className="text-muted-foreground">{told[l.id].says}</p>
          <StatusBadge type="success">{told[l.id].spared}</StatusBadge>
        </div>
        <ChecklistItems
          key={l.id}
          items={l.points}
          className={cn("content-start text-sm md:pt-7", rise)}
        />
      </div>
    ) : (
      <div
        key={pos}
        className={cn(
          panel,
          "grid h-full gap-6 p-8 md:grid-cols-[1fr_42rem] md:gap-12",
        )}
      >
        <div className={cn("grid content-start gap-3", rise)}>
          <p className="text-xs text-primary">
            {pos === START ? "Where it starts" : "Where it ends"}
          </p>
          <h2 className="flex items-center gap-3 font-heading text-2xl font-semibold [&_svg]:size-6 [&_svg]:text-primary">
            {pos === START ? <Building2 /> : <AppWindowIcon />}
            {pos === START ? "Who’s signing in?" : "Who’s in the app?"}
          </h2>
          <p className="text-muted-foreground">
            {pos === START
              ? "With the account they already use at work. No new password, no account to create."
              : "It receives who the person is and their team from shpyrd, on every request."}
          </p>
          <StatusBadge type="success">
            {pos === START
              ? "No password for the app"
              : "No sign-in code written"}
          </StatusBadge>
        </div>
        <ChecklistItems
          key={pos}
          items={
            pos === START
              ? [
                  "Google Workspace, Microsoft Entra, Okta, GitHub…",
                  `As ${EMAIL}`,
                  "Then the request crosses five steps",
                ]
              : [
                  `Who: ${EMAIL}`,
                  "Team: Finance",
                  "What they may do: open it, or change it",
                ]
          }
          className={cn("content-start text-sm md:pt-7", rise)}
        />
      </div>
    );
  };

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Layers />}>{platform.label}</Label>}
        heading={
          <>
            A platform where{" "}
            <span className="text-primary">every app starts out safe</span>
          </>
        }
        lead={platform.lead}
      />

      {/* The journey on the slide's ground, out of a card (Giovani, 2026-10-28); the stop's card under it. */}
      <div ref={way} className="relative grid gap-6 pt-8">
        {/* A dotted grey thread from the place the request is at down to
            its card (Giovani, 2026-10-28): from under the stop, down, across with rounded corners,
            to the middle of the "logs, metrics" pill, then straight down
            from the pill to the card; it follows the request along. */}
        {thread && (
          <svg
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 size-full overflow-visible max-md:hidden"
          >
            <path
              d={thread}
              fill="none"
              className="stroke-muted-foreground/40 transition-[d] duration-slow ease-move"
              strokeWidth={1.5}
              strokeDasharray="1 5"
              strokeLinecap="round"
              style={{ d: `path("${thread}")` }}
            />
          </svg>
        )}
        {/* The journey: who, the stops, the app. */}
        <div
          data-journey=""
          className="relative grid items-start gap-6 md:grid-cols-[auto_1fr_auto]"
        >
          {/* The line runs on from window to window (Giovani, 2026-10-28):
              grey under everything, lit from the sign-in to the first stop
              once the request sets off, and from the last stop to the app
              when it arrives. */}
          <span
            aria-hidden="true"
            className="absolute top-8 right-0 left-0 h-px bg-border max-md:hidden"
          />
          <span
            aria-hidden="true"
            className={cn(
              "absolute top-8 left-52 h-px w-6 origin-left max-md:hidden bg-primary transition-transform duration-slow ease-move",
              at === START ? "scale-x-0" : "scale-x-100",
            )}
          />
          {/* The request itself, along the whole way (Giovani, 2026-10-28):
              it comes out from behind the sign-in window, halts at each
              stop and goes in behind the app's window. The windows are
              208 px wide and 24 px off the stops' columns. */}
          <span
            aria-hidden="true"
            className="absolute top-8 z-[5] size-2.5 max-md:hidden -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary shadow-[0_0_12px_3px_rgb(255_79_0/0.5)] transition-[left] duration-slow ease-move motion-safe:animate-pulse"
            style={{
              left:
                at === START
                  ? "104px"
                  : at === END
                    ? "calc(100% - 104px)"
                    : `calc(232px + (100% - 464px) * ${(at + 0.5) / stops})`,
            }}
          />
          <span
            aria-hidden="true"
            className={cn(
              "absolute top-8 right-52 h-px w-6 origin-left max-md:hidden bg-primary transition-transform duration-slow ease-move",
              at === END ? "scale-x-100" : "scale-x-0",
            )}
          />
          {/* The start: someone signs in with the company's account
              (Giovani, 2026-10-28), the email typed out and the button
              pressed; then the request sets off. */}
          <button
            type="button"
            data-place={START}
            {...mouseOnly}
            onClick={() => go(START)}
            aria-label="Start: signing in"
            className={cn(
              // The site's window, as the app's at the end (Giovani, 2026-10-28).
              glass,
              "relative z-10 grid w-52 cursor-pointer rounded-2xl p-1.5 max-md:hidden text-left transition-[box-shadow] duration-normal",
              at === START ? "ring-primary/50" : "hover:ring-foreground/20",
            )}
          >
            <span className="grid overflow-hidden rounded-2xl border border-white/80 bg-background bg-linear-to-b from-background to-muted dark:border-white/10 dark:from-card dark:to-background">
              <span className="flex items-center gap-1 border-b border-foreground/6 px-3 py-2">
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="ml-1.5 text-[11px] text-muted-foreground">
                  yourapp.com
                </span>
              </span>
              <span className="grid gap-3 p-4">
                <span className="text-xs font-medium text-muted-foreground">
                  Sign in to yourapp
                </span>
                <span className="flex h-8 items-center rounded-md border border-border bg-background px-2.5 text-xs">
                  {EMAIL.slice(0, typed)}
                  {at === START && typed < EMAIL.length && (
                    <span className="ml-px h-3.5 w-px animate-pulse bg-foreground" />
                  )}
                </span>
                <span
                  className={cn(
                    "flex h-8 items-center justify-center gap-1.5 rounded-md text-xs font-medium transition-[background-color,color,scale] duration-fast [&_svg]:size-3.5",
                    signedIn
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted text-muted-foreground",
                    at === START && signedIn && "scale-95",
                  )}
                >
                  <Building2 />
                  Company sign-in
                </span>
              </span>
            </span>
          </button>

          <div className="relative">
            {/* The track, and how far the request has come. */}
            <span
              aria-hidden="true"
              className="absolute top-8 max-md:top-6 right-0 left-0 h-px bg-border"
            />
            <span
              aria-hidden="true"
              className="absolute top-8 max-md:top-6 left-0 h-px bg-primary transition-[width] duration-slow ease-move"
              style={{ width: `${head}%` }}
            />

            <div
              role="tablist"
              aria-label={platform.label}
              // On a phone the windows go and the stops alone, smaller, keep
              // the way; their questions are on the card.
              className="relative grid grid-cols-5 max-md:[&_button>span:first-child]:size-12 max-md:[&_button>span:first-child_svg]:size-5"
            >
              {platform.layers.map((l, i) => {
                const done = i < at;
                const here = i === at;
                return (
                  <button
                    key={l.id}
                    type="button"
                    role="tab"
                    aria-selected={here}
                    data-place={i}
                    {...mouseOnly}
                    onClick={() => go(i)}
                    className="group grid cursor-pointer content-start justify-items-center gap-3 rounded-2xl px-1 text-center outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {/* The site's route (route-timeline.tsx): a 64 px rounded box,
                        the icon orange on white; the stop the request is at
                        filled orange. */}
                    <span
                      className={cn(
                        "relative z-20 flex size-16 items-center justify-center rounded-2xl ring-1 transition-[background-color,color,box-shadow,translate] duration-normal ease-move group-hover:-translate-y-0.5 [&_svg]:size-7",
                        here
                          ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                          : "bg-background ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
                        !here &&
                          (done
                            ? "text-primary group-hover:ring-primary/40"
                            : "text-muted-foreground group-hover:text-primary group-hover:ring-primary/40"),
                      )}
                    >
                      {icons[l.id]}
                      {/* Its number, as the card under the journey says it. */}
                      <span
                        className={cn(
                          "absolute -top-2 -right-2 grid size-5 place-items-center rounded-full text-[10px] font-semibold tabular-nums ring-2 ring-(--page-ground) transition-colors",
                          here
                            ? "bg-foreground text-background"
                            : "bg-muted text-muted-foreground",
                        )}
                      >
                        {i + 1}
                      </span>
                    </span>
                    <span className="text-base font-semibold text-foreground max-md:text-xs">
                      {l.name}
                    </span>
                    <span
                      className={cn(
                        "text-sm text-balance transition-colors max-md:hidden",
                        here ? "text-primary" : "text-muted-foreground",
                      )}
                    >
                      {told[l.id].question}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* The end: the app opens, already knowing who is there and
              their team, lit when the request arrives. */}
          <button
            type="button"
            data-place={END}
            {...mouseOnly}
            onClick={() => go(END)}
            aria-label="End: in the app"
            className={cn(
              // The site's window (screen.tsx): a glass frame round the
              // window, its edge white, its ground fading to grey, its bar a
              // faint line (Giovani, 2026-10-28).
              glass,
              "relative z-10 grid w-52 cursor-pointer rounded-2xl p-1.5 max-md:hidden text-left transition-[box-shadow] duration-normal",
              at === END ? "ring-primary/50" : "hover:ring-foreground/20",
            )}
          >
            <span className="grid overflow-hidden rounded-2xl border border-white/80 bg-background bg-linear-to-b from-background to-muted dark:border-white/10 dark:from-card dark:to-background">
              <span className="flex items-center gap-1 border-b border-foreground/6 px-3 py-2">
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="size-1.5 rounded-full bg-foreground/15" />
                <span className="ml-1.5 text-[11px] text-muted-foreground">
                  yourapp.com
                </span>
              </span>
              <span className="grid gap-2 p-4">
                <span
                  className={cn(
                    "text-sm font-semibold transition-colors",
                    at === END ? "text-foreground" : "text-muted-foreground",
                  )}
                >
                  Hello, team
                </span>
                <span className="text-xs leading-snug text-muted-foreground">
                  {EMAIL}
                </span>
                <StatusBadge
                  type={at === END ? "success" : "neutral"}
                  className="w-fit"
                >
                  Finance team
                </StatusBadge>
              </span>
            </span>
          </button>
        </div>

        {/* Along the whole way. */}
        <p
          data-logs=""
          className="relative z-10 mx-auto mt-8 flex w-fit items-center gap-2 rounded-full max-md:mt-0 max-md:rounded-2xl border border-dashed border-border bg-(--page-ground) px-4 py-2 text-xs text-muted-foreground [&_svg]:size-3.5 [&_svg]:text-primary"
        >
          <Activity />
          {platform.across}
        </p>

        {/* The stop the request is at, or where it starts and ends. Every
            card the same size (Giovani, 2026-10-28): all of them laid in one
            cell, the one in view shown, so the cell takes the tallest; as
            wide as they need, centred. */}
        <div data-cards="" className="relative z-10 grid w-full">
          {Array.from({ length: stops + 2 }, (_, k) => k - 1).map((pos) => (
            <div
              key={pos}
              role={pos === at ? "tabpanel" : undefined}
              aria-hidden={pos === at ? undefined : true}
              className={cn("grid [grid-area:1/1]", pos !== at && "invisible")}
            >
              {card(pos)}
            </div>
          ))}
        </div>

        {/* Going along it by hand, in the design system's parts and the
            deck's own (Giovani, 2026-10-28): outline icon buttons, the
            deck's chevrons, and play and pause filled as the VideoPlayer
            draws them. */}
        <div className="flex items-center justify-center gap-2">
          <Button
            variant="outline"
            size="icon-lg"
            className={stepButton}
            {...mouseOnly}
            onClick={() => go(at - 1)}
            aria-label="Previous step"
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
            aria-label="Next step"
          >
            <ChevronRight />
          </Button>
        </div>
      </div>
    </div>
  );
}
