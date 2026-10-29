"use client";

// Slide 6 (Giovani, 2026-10-28, from the proposal at
// /proposals/access-scenarios): not steps, three scenarios side by side. The same app, three people, each in a card along the top
// with the outcome on it; under them the browser of the one chosen, in the
// middle, between what the front door checked (left) and what the app is
// told (right). A dotted thread runs from the chosen person down to the
// browser. Left alone it moves from one person to the next.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  DoorOpen,
  Fingerprint,
  Minus,
  Pause,
  Play,
  ShieldCheck,
  ShieldX,
  X,
} from "lucide-react";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { access } from "@/content/tour-v3";
import { Screen } from "@/components/screen";
import { AccessView } from "./access";
import { PersonMark, type Look } from "./person-mark";
import { Intro, Label, panel, rise, mouseOnly } from "./parts";

// Nothing under the people at first (Giovani, 2026-10-28): the orange line
// grows from the person and brings in each box in turn, the checks, the
// browser, what the app is told; then time to read them all.
const HOLD = 11000;
const beat = (ms: number) => ({ animationDelay: `${ms}ms` });
const stepButton = "hover:border-primary/40 hover:text-primary";

// What the front door checks, for each person: their account, then their
// team; and the outcome, in a word.
const checks: Record<
  string,
  { account: boolean | null; team: boolean | null; outcome: string }
> = {
  finance: { account: true, team: true, outcome: "Gets in" },
  marketing: { account: true, team: false, outcome: "Turned away" },
  outside: { account: false, team: null, outcome: "Never reaches the app" },
};

function Mark({ ok }: { ok: boolean | null }) {
  if (ok === null)
    return (
      <Minus
        aria-label="not checked"
        className="size-4 text-muted-foreground"
      />
    );
  return ok ? (
    <Check aria-label="yes" className="size-4 text-success" strokeWidth={2.5} />
  ) : (
    <X aria-label="no" className="size-4 text-destructive" strokeWidth={2.5} />
  );
}

export function Access() {
  const [who, setWho] = useState(0);
  const [moving, setMoving] = useState(true);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches)
      setMoving(false);
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(
      () => setWho((w) => (w + 1) % access.people.length),
      HOLD,
    );
    return () => window.clearTimeout(timer);
  }, [moving, who]);
  const go = (i: number) => {
    setWho((i + access.people.length) % access.people.length);
    setMoving(false);
  };
  const person = access.people[who];
  const check = checks[person.id];

  // The thread: from under the chosen person's card, down, across with
  // rounded corners, to the top of the browser's frame, in its middle.
  // Which box the thread runs to, in time with their coming in.
  const [phase, setPhase] = useState(0);
  useEffect(() => {
    // From empty, so the first piece fills too.
    setPhase(-1);
    const timers = [
      window.setTimeout(() => setPhase(0), 60),
      window.setTimeout(() => setPhase(1), 3000),
      window.setTimeout(() => setPhase(2), 5300),
    ];
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [who]);
  const box = useRef<HTMLDivElement>(null);
  // The thread grows one way, as the story goes (Giovani, 2026-10-28):
  // from the person into the front door's checks; out of them into the
  // browser; out of the browser into what the app receives.
  const [thread, setThread] = useState<string[] | null>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const measure = () => {
      // Where each box sits in the layout, in the box's own pixels, not
      // where it shows: they rise in from a little lower.
      const o = { left: 0, top: 0 };
      const rel = (e: Element | null | undefined) => {
        if (!(e instanceof HTMLElement)) return undefined;
        let x = 0;
        let y = 0;
        let n: HTMLElement | null = e;
        while (n && n !== el) {
          x += n.offsetLeft;
          y += n.offsetTop;
          n = n.offsetParent as HTMLElement | null;
        }
        const w = e.offsetWidth;
        const h = e.offsetHeight;
        return {
          left: x,
          top: y,
          width: w,
          height: h,
          right: x + w,
          bottom: y + h,
        };
      };
      const at = (q: string) => rel(el.querySelector(q));
      const from = at(`[data-person="${who}"]`);
      const checks = at("[data-box=checks]");
      const screen = rel(
        el.querySelector("[data-slot=browser-frame]")?.parentElement,
      );
      const told = at("[data-box=told]");
      if (!from || !checks || !screen || !told) return;
      // Down from the person, across with rounded corners, into the top of
      // the checks.
      const x0 = from.left + from.width / 2 - o.left;
      const y0 = from.bottom - o.top;
      const x1 = checks.left + checks.width / 2 - o.left;
      const y1 = checks.top - o.top;
      const my = (y0 + y1) / 2;
      const dir = Math.sign(x1 - x0);
      const r = Math.min(12, Math.abs(x1 - x0) / 2, (y1 - y0) / 2);
      const down =
        `M ${x0} ${y0} L ${x0} ${my - r} Q ${x0} ${my} ${x0 + dir * r} ${my} ` +
        `L ${x1 - dir * r} ${my} Q ${x1} ${my} ${x1} ${my + r} L ${x1} ${y1}`;
      // Then out of the foot of the checks, in its middle, down and round
      // a corner into the browser's side; and, as its mirror, out of the
      // browser's other side and round a corner up into the foot of what
      // the app receives (Giovani, 2026-10-28).
      const R = 12;
      const cx = checks.left + checks.width / 2 - o.left;
      const yb = checks.bottom - o.top + 28;
      const sl = screen.left - o.left;
      const across1 =
        `M ${cx} ${checks.bottom - o.top} L ${cx} ${yb - R} ` +
        `Q ${cx} ${yb} ${cx + R} ${yb} L ${sl} ${yb}`;
      const tx = told.left + told.width / 2 - o.left;
      const yc = told.bottom - o.top + 28;
      const sr = screen.right - o.left;
      const across2 =
        `M ${sr} ${yc} L ${tx - R} ${yc} ` +
        `Q ${tx} ${yc} ${tx} ${yc - R} L ${tx} ${told.bottom - o.top}`;
      setThread([down, across1, across2]);
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(el);
    const late = [300, 1200].map((t) => window.setTimeout(measure, t));
    return () => {
      seen.disconnect();
      late.forEach((t) => window.clearTimeout(t));
    };
  }, [who, phase]);

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Fingerprint />}>{access.label}</Label>}
        heading={access.heading}
        lead="Three people open the same app. shpyrd's front door decides who gets through, and tells the app who they are, so the app never handles a password."
      />

      <div ref={box} className="relative grid gap-16 pt-8 max-md:gap-6 max-md:pt-2">
        {thread && (
          <svg
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 z-30 size-full overflow-visible max-lg:hidden"
          >
            {/* As slide 4's journey's orange (Giovani, 2026-10-28): no line
                at first; it grows, piece by piece, each ahead of its box. */}
            {/* Keyed by the person: on the next one the line is gone at
                once, and grows again (Giovani, 2026-10-28). */}
            {thread.map((d, i) => (
              <g key={`${who}-${i}`}>
                <path
                  d={d}
                  fill="none"
                  pathLength={1}
                  strokeDasharray="1 1"
                  className="stroke-primary transition-[stroke-dashoffset] duration-900 ease-move"
                  strokeWidth={1}
                  style={{ strokeDashoffset: phase >= i ? 0 : 1 }}
                />
              </g>
            ))}
          </svg>
        )}

        {/* The three people, side by side, each with the outcome. */}
        <div
          role="radiogroup"
          aria-label="Who opens the app"
          className="grid gap-4 md:grid-cols-3"
        >
          {access.people.map((p, i) => {
            const here = i === who;
            const c = checks[p.id];
            return (
              <button
                key={p.id}
                type="button"
                role="radio"
                aria-checked={here}
                data-person={i}
                {...mouseOnly}
                onClick={() => go(i)}
                className={cn(
                  panel,
                  "group relative flex cursor-pointer items-center gap-4 overflow-hidden px-4 py-4 text-left transition-[border-color,background-color] duration-normal ease-move hover:border-primary/40 focus-visible:ring-2 focus-visible:ring-ring",
                  here && "border-primary/50 bg-primary/5",
                )}
              >
                <span
                  className={cn(
                    "flex size-16 shrink-0 items-center justify-center rounded-2xl bg-background ring-1 transition-[box-shadow,translate] duration-normal ease-move group-hover:-translate-y-0.5 dark:bg-card",
                    here
                      ? "ring-2 ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                      : "ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] group-hover:ring-primary/40",
                  )}
                >
                  <PersonMark look={p.look as Look} />
                </span>
                <span className="grid min-w-0 gap-1">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="text-base font-semibold text-foreground">
                      {p.name}
                    </span>
                    <Badge variant="secondary" className="shrink-0">
                      {p.team}
                    </Badge>
                  </span>
                  <span className="truncate text-sm text-muted-foreground">
                    {p.email}
                  </span>
                  <StatusBadge
                    type={p.allowed ? "success" : "error"}
                    className="mt-1"
                  >
                    {c.outcome}
                  </StatusBadge>
                </span>
                {here && moving && (
                  <span
                    aria-hidden="true"
                    className="absolute inset-x-4 bottom-1.5 h-1 overflow-hidden rounded-full bg-primary/15"
                  >
                    <span
                      key={who}
                      className="block h-full rounded-full bg-primary motion-safe:animate-[tour-fill-x_linear_forwards]"
                      style={{ animationDuration: `${HOLD}ms` }}
                    />
                  </span>
                )}
              </button>
            );
          })}
        </div>

        {/* What happens: the front door's checks, the browser, what the app gets. */}
        <div className="grid items-start gap-6 lg:grid-cols-[15rem_1fr_15rem]">
          <div
            key={`checks-${person.id}`}
            data-box="checks"
            className={cn(glass, "grid gap-3 rounded-2xl p-4 text-sm", rise)}
            style={beat(900)}
          >
            <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground [&_svg]:size-3.5 [&_svg]:text-primary">
              <DoorOpen />
              What the front door checks
            </p>
            <p
              className={cn("flex items-center gap-2.5", rise)}
              style={beat(1300)}
            >
              <Mark ok={check.account} />
              Company account
            </p>
            <p
              className={cn("flex items-center gap-2.5", rise)}
              style={beat(1900)}
            >
              <Mark ok={check.team} />
              Team allowed on the app
            </p>
            <p
              className={cn(
                "border-t border-border pt-3 text-muted-foreground",
                rise,
              )}
              style={beat(2500)}
            >
              {person.verdict}
            </p>
          </div>

          {/* The browser too comes in at its turn, frame and all. */}
          <div key={`screen-${person.id}`} className={rise} style={beat(3900)}>
            <Screen
              address={
                person.id === "outside"
                  ? "https://apps.yourapp.com/login"
                  : access.url
              }
              className="min-h-72"
            >
              {/* All three views in one cell, only this person's shown, so
                  the browser is as tall as the tallest of them whoever is
                  chosen, and the controls under the slide hold still
                  (Giovani, 2026-10-28). */}
              <div className="grid min-h-64">
                {access.people.map((p) => (
                  <div
                    key={p.id}
                    aria-hidden={p.id === person.id ? undefined : true}
                    className={cn(
                      "grid [grid-area:1/1]",
                      p.id !== person.id && "invisible",
                    )}
                  >
                    <AccessView person={p} />
                  </div>
                ))}
              </div>
            </Screen>
          </div>

          <div
            key={`told-${person.id}`}
            data-box="told"
            className={cn(glass, "grid gap-3 rounded-2xl p-4", rise)}
            style={beat(6200)}
          >
            <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground [&_svg]:size-3.5">
              {person.allowed ? (
                <ShieldCheck className="text-success" />
              ) : (
                <ShieldX className="text-destructive" />
              )}
              {access.headersLabel}
            </p>
            {person.allowed ? (
              <dl className="grid gap-2 text-xs">
                {[
                  ["Who", person.email],
                  ["Name", "Marina Costa"],
                  ["Teams", "finance, everyone"],
                  ["Role", "user"],
                ].map(([k, v]) => (
                  <div key={k} className="grid gap-0.5">
                    <dt className="text-muted-foreground">{k}</dt>
                    <dd className="font-mono">{v}</dd>
                  </div>
                ))}
              </dl>
            ) : (
              <p className="text-sm text-muted-foreground">
                Nothing. The request never reached the app.
              </p>
            )}
            <p className="border-t border-border pt-3 text-xs text-muted-foreground">
              Sent as headers on every request: the app reads them, it keeps no
              passwords.
            </p>
          </div>
        </div>
      </div>

      <div className="-mt-2 flex items-center justify-center gap-2">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => go(who - 1)}
          aria-label="Previous person"
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
          onClick={() => go(who + 1)}
          aria-label="Next person"
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
