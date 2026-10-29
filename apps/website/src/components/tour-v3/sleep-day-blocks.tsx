"use client";

// The blocks version of the slide 7 proposal (Giovani, 2026-10-28): the day
// drawn as the home's "Pay only for what it uses" film draws an agent's CPU,
// and its values in that film's meters. The line version is sleep-day.tsx.
//
// From sleep-day.tsx: the same day of an internal
// app, made to explain itself. On the chart: what you don't pay, the gap
// between "always on" and what shpyrd runs, washed in green; the day's three
// moments marked as the home's metrics film marks its releases (it wakes at
// the first visit, a second copy starts at the peak, it sleeps when nobody is
// left); plain words for the lines. Under it the app's state as the home's
// sleep film tells it (awake or asleep, what it costs now). Beside it the
// month, in the design system's ProgressBar and Stat. The day plays and can
// be paused, or read hour by hour under the pointer.
import { useEffect, useId, useRef, useState } from "react";
import {
  ChartNoAxesColumnIncreasing,
  Moon,
  Pause,
  Play,
  Sun,
  Users,
  Layers,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { Stat } from "@shpyrd/ui/components/stat";
import { KnobSlider } from "@shpyrd/ui/components/knob-slider";
import { Switch } from "@shpyrd/ui/components/switch";
import { sleep } from "@/content/tour-v3";
import { Heading, Label, Lead, mouseOnly, panel } from "./parts";

// People online at an hour of the day (8.5 is half past eight).
function people(h: number) {
  const bump = (centre: number, width: number, height: number) =>
    height * Math.exp(-(((h - centre) / width) ** 2));
  return h >= 8.5 && h <= 18.5 ? bump(11, 2.2, 34) + bump(15.5, 2, 28) : 0;
}
// An instance for every eight people, kept for a quarter of an hour after
// the load drops; none at all once nobody has been there for that long.
const PER = 8;
function instances(h: number) {
  let most = 0;
  for (let back = 0; back <= 0.25; back += 0.05)
    most = Math.max(most, Math.ceil(people(h - back) / PER));
  return most;
}
// The day, every three minutes: enough for a smooth curve.
const samples = Array.from({ length: 481 }, (_, i) => i / 20);
const PEAK_INSTANCES = Math.max(...samples.map(instances));
const instanceShare =
  samples.reduce((n, h) => n + instances(h), 0) /
  samples.length /
  PEAK_INSTANCES;
// An hour of the day as a clock says it: 8.5 is 08:30.
const clock = (h: number) =>
  `${String(Math.floor(h)).padStart(2, "0")}:${String(Math.floor((h % 1) * 60)).padStart(2, "0")}`;
// How long the day takes to go by, in the browser, and where it starts.
const DAY_MS = 16000;
const START = 3.5;

// An illustrative month: what an app and its database cost always on.
const APP = 18;
const DATABASE = 22;
const DISK = 2;
const money = (n: number) => `$${Math.round(n).toLocaleString("en")}`;

// The day in blocks, as the home's cost film: a column for every half hour,
// two blocks for every copy running. Its faint outline is what an app kept
// on, sized for the peak, holds all day; the orange what shpyrd runs. At
// night the column is empty: the app sleeps.
const COLUMNS = 48;
const PER_COPY = 2;
const STACK = PEAK_INSTANCES * PER_COPY;
const copies = Array.from({ length: COLUMNS }, (_, i) =>
  Math.max(
    ...samples.filter((h) => h >= i / 2 && h < (i + 1) / 2).map(instances),
  ),
);
const column = (h: number) => Math.min(COLUMNS - 1, Math.floor(h * 2));
// Where an hour's column sits across the chart, in percent: its centre.
const across = (h: number) => ((column(h) + 0.5) / COLUMNS) * 100;
// The day's moments: when it wakes, when it reaches its peak, when it
// sleeps again.
const WAKE = samples.find((h) => instances(h) > 0) ?? 9;
const PEAK_AT = samples.find((h) => instances(h) === PEAK_INSTANCES) ?? 11;
const SLEEP = [...samples].reverse().find((h) => instances(h) > 0) ?? 18.5;
const moments = [
  { at: WAKE, title: clock(WAKE), text: "First visit: it wakes in seconds" },
  {
    at: PEAK_AT,
    title: clock(PEAK_AT),
    text: `The peak: ${PEAK_INSTANCES} copies run`,
  },
  { at: SLEEP, title: clock(SLEEP), text: "Nobody left: it sleeps" },
];
// What a copy of an app costs an hour, from its month.
const HOURLY = APP / 730;
const stepButton = "hover:border-primary/40 hover:text-primary";
// Surfaces of data, resting on the slide: no shadow (Giovani, 2026-10-28).
const card = panel;

// The words as the home's cost film writes them (Giovani, 2026-10-28): a
// name in the foreground, medium, and beside it a muted caption; the meters
// on a light wash, shpyrd's orange, the always-on one grey, each a muted
// label and a semibold value. Captions 12 px, the cards' labels 14 px.
export function SleepDayBlocks() {
  const [apps, setApps] = useState(47);
  const [databases, setDatabases] = useState(true);
  const slider = useId();
  const [playing, setPlaying] = useState(true);

  // The hour shown: the day going by, or the one under the pointer.
  const [hour, setHour] = useState(START);
  const [pointer, setPointer] = useState<number | null>(null);
  const chart = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setPlaying(false);
      setHour(23.99);
    }
  }, []);
  useEffect(() => {
    if (!playing || pointer !== null) return;
    let frame = 0;
    let last = performance.now();
    const tick = (t: number) => {
      const dt = t - last;
      last = t;
      setHour((h) => (h + (dt / DAY_MS) * 24) % 24);
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [playing, pointer]);
  const h = pointer ?? hour;
  const now = column(h);
  const n = copies[now];
  const awake = n > 0;
  // The day so far, for this app: what shpyrd billed, what always on would.
  const billed =
    copies.slice(0, now + 1).reduce((a, c) => a + c, 0) * 0.5 * HOURLY;
  const kept = (now + 1) * 0.5 * PEAK_INSTANCES * HOURLY;

  const always = apps * (APP + DATABASE);
  const ours =
    apps *
    (APP * instanceShare +
      (databases ? DATABASE * instanceShare + DISK : DATABASE));
  const saved = Math.round((1 - ours / always) * 100);

  return (
    <div className="grid gap-8">
      <div className="grid gap-4">
        <Label icon={<ChartNoAxesColumnIncreasing />}>{sleep.label}</Label>
        <Heading>
          {sleep.heading[0]}{" "}
          <span className="text-primary">{sleep.heading[1]}</span>
        </Heading>
        <Lead>
          Apps and databases sleep when nobody uses them, wake on the first
          visit in seconds, and add capacity at the peak. Nobody configures a
          thing.
        </Lead>
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <div className={cn(card, "grid gap-4 p-5")}>
          {/* As the home's film: the app's name, and beside it the clock
              going by and what the app is doing, on a pill (Giovani,
              2026-10-28: more weight), orange while awake, grey asleep. */}
          <div className="flex items-center justify-between gap-3 max-sm:flex-col max-sm:items-stretch">
            <span className="font-medium text-foreground">
              expenses · an internal app
            </span>
            <span
              className={cn(
                "inline-flex items-center gap-3 rounded-md px-3 py-1.5 text-sm whitespace-nowrap max-sm:justify-between max-sm:gap-2 max-sm:px-2.5 max-sm:text-xs [&_svg]:size-4 [&_svg]:shrink-0",
                awake
                  ? "bg-primary/[0.07] text-primary"
                  : "bg-foreground/[0.04] text-muted-foreground",
              )}
            >
              {/* Each piece with its icon (Giovani, 2026-10-28): the hour,
                  the people there, the copies that serve them; a thin line
                  between them, and each a fixed width so the pill keeps its
                  size as the day goes by. */}
              <span className="inline-flex items-center gap-1.5">
                {awake ? <Sun /> : <Moon />}
                <span className="text-lg font-semibold text-foreground tabular-nums max-sm:text-base">
                  {clock(h)}
                </span>
              </span>
              <span aria-hidden className="h-4 w-px bg-current opacity-20" />
              <span className="inline-flex items-center sm:w-[5.5rem] gap-1.5 font-medium text-muted-foreground tabular-nums">
                <Users />
                {Math.round(people(h))} people
              </span>
              <span aria-hidden className="h-4 w-px bg-current opacity-20" />
              <span className="inline-flex items-center sm:w-[8.125rem] gap-1.5 font-medium tabular-nums">
                <Layers />
                {awake
                  ? `Awake, ${n} ${n === 1 ? "copy" : "copies"}`
                  : "Asleep, 0 copies"}
              </span>
            </span>
          </div>

          <div>
            {/* The moments' labels above the blocks, in one row, each
                beside its line (the peak's to the right). */}
            <div className="relative h-11 max-sm:h-8">
              {moments.map((m, i) => (
                <div
                  key={m.text}
                  className="pointer-events-none absolute inset-y-0"
                  style={{ left: `${across(m.at)}%` }}
                >
                  <span
                    className={cn(
                      "absolute top-0 grid w-max rounded-md bg-background px-2 py-1 text-xs leading-tight shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8",
                      i === 1 ? "-left-3" : "-right-3",
                    )}
                  >
                    <span className="font-medium text-foreground tabular-nums">
                      {m.title}
                    </span>
                    <span className="text-muted-foreground max-sm:hidden">{m.text}</span>
                  </span>
                  <span className="absolute top-9 bottom-0 left-0 border-l-[1.5px] max-sm:top-6 border-dashed border-primary/80" />
                </div>
              ))}
            </div>

            {/* The day in blocks, filled as far as the day has gone. */}
            <div
              ref={chart}
              className="relative flex h-48 cursor-crosshair items-end gap-[2px] pt-3"
              role="img"
              aria-label="A day of an internal app"
              onMouseMove={(e) => {
                const r = chart.current?.getBoundingClientRect();
                if (!r) return;
                setPointer(
                  Math.min(
                    23.99,
                    Math.max(0, ((e.clientX - r.left) / r.width) * 24),
                  ),
                );
              }}
              onMouseLeave={() => {
                if (pointer !== null) setHour(pointer);
                setPointer(null);
              }}
            >
              {copies.map((c, i) => (
                <div
                  key={i}
                  className="flex h-full min-w-0 flex-1 flex-col-reverse gap-[2px]"
                >
                  {Array.from({ length: STACK }, (_, b) => (
                    <span
                      key={b}
                      className={cn(
                        "min-h-0 flex-1 rounded-[1px] transition-colors duration-300",
                        i <= now && b < c * PER_COPY
                          ? i === now
                            ? "bg-primary"
                            : "bg-primary/85"
                          : i === now
                            ? "bg-foreground/[0.12]"
                            : "bg-foreground/[0.06]",
                      )}
                    />
                  ))}
                </div>
              ))}
              {/* The moments' lines, down through the blocks. */}
              {moments.map((m) => (
                <span
                  key={m.text}
                  className="pointer-events-none absolute top-0 bottom-0 border-l-[1.5px] border-dashed border-primary/80"
                  style={{ left: `${across(m.at)}%` }}
                />
              ))}
            </div>
            <div className="mt-2 flex justify-between text-xs text-muted-foreground tabular-nums">
              {[0, 6, 12, 18, 24].map((t) => (
                <span key={t}>{String(t).padStart(2, "0")}h</span>
              ))}
            </div>
          </div>

          {/* The day so far, in the home film's two meters. */}
          <div className="grid gap-3 sm:grid-cols-2">
            <Meter label="On shpyrd, by use" tone="ours">
              ${billed.toFixed(2)}
            </Meter>
            <Meter label="Kept on all day" tone="kept">
              ${kept.toFixed(2)}
            </Meter>
          </div>
        </div>

        {/* The month, for all the company's apps. */}
        <div className={cn(card, "grid content-start gap-5 p-5")}>
          <div className="grid gap-3">
            <label htmlFor={slider} className="text-sm text-muted-foreground">
              How many apps your company has
            </label>
            <KnobSlider
              id={slider}
              min={5}
              max={150}
              value={apps}
              onValueChange={setApps}
              tabIndex={-1}
            />
          </div>
          <label className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
            {sleep.databases}
            <Switch
              checked={databases}
              onCheckedChange={setDatabases}
              statusLabel={false}
            />
          </label>
          <div className="grid gap-3 border-t border-border pt-4">
            <Meter label="On shpyrd, a month" tone="ours">
              {money(ours)}
            </Meter>
            <Meter label="Kept on, a month" tone="kept">
              {money(always)}
            </Meter>
          </div>
          <Stat
            className="border-t border-border pt-4"
            label="What you save a month"
            value={money(always - ours)}
            delta={{
              value: `${saved}%`,
              direction: "down",
              good: true,
              against: "of the bill · an illustrative estimate",
            }}
            tone="success"
          />
        </div>
      </div>

      {/* As the other slides: play or pause the day. */}
      <div className="-mt-2 flex items-center justify-center">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => setPlaying((p) => !p)}
          aria-label={playing ? "Pause" : "Play"}
        >
          {playing ? (
            <Pause className="fill-current" />
          ) : (
            <Play className="translate-x-px fill-current" />
          )}
        </Button>
      </div>
    </div>
  );
}

// A meter as the home's cost film draws it: on one line, what it is, muted,
// and what it costs, semibold; shpyrd's on an orange wash, the other grey.
function Meter({
  label,
  tone,
  children,
}: {
  label: string;
  tone: "ours" | "kept";
  children: React.ReactNode;
}) {
  return (
    <p
      className={cn(
        "flex items-baseline justify-between gap-3 rounded-md px-3 py-2",
        tone === "ours" ? "bg-primary/[0.07]" : "bg-foreground/[0.04]",
      )}
    >
      <span className="truncate text-sm text-muted-foreground">{label}</span>
      <span
        className={cn(
          "text-lg font-semibold tabular-nums",
          tone === "ours" ? "text-primary" : "text-foreground/70",
        )}
      >
        {children}
      </span>
    </p>
  );
}
