"use client";

// A proposal for slide 7 (Giovani, 2026-10-28): the same day of an internal
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
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { Button } from "@shpyrd/ui/components/button";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Stat } from "@shpyrd/ui/components/stat";
import { KnobSlider } from "@shpyrd/ui/components/knob-slider";
import { Switch } from "@shpyrd/ui/components/switch";
import { sleep } from "@/content/tour-v3";
import { Heading, Label, Lead, mouseOnly } from "./parts";

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

// The drawing: a box of 960 by 220, people on the same scale as the
// instances that serve them (eight to an instance).
const W = 960;
const H = 220;
const TOP = 24;
const PEAK = (PEAK_INSTANCES + 0.5) * PER;
const x = (h: number) => (h / 24) * W;
const y = (n: number) => H - (n / PEAK) * (H - TOP);
const curve = samples
  .map((h) => `${x(h).toFixed(1)},${y(people(h)).toFixed(1)}`)
  .join(" L");
const area = `M0,${H} L${curve} L${W},${H} Z`;
const level = (h: number) => y(instances(h) * PER);
// The instances as steps with rounded corners (2026-10-20, as the home's
// charts: no dry corners), 8 units of the drawing round each turn.
const R = 8;
const turns = samples.flatMap((h, i) => {
  if (i === 0) return [];
  const before = level(samples[i - 1]);
  const now = level(h);
  return now === before ? [] : [{ at: x(h), before, now }];
});
const steps = turns
  .reduce(
    (d, { at, before, now }) => {
      const dir = now > before ? 1 : -1;
      const r = Math.min(R, Math.abs(now - before) / 2);
      return `${d} L${(at - r).toFixed(1)},${before.toFixed(1)} Q${at.toFixed(1)},${before.toFixed(1)} ${at.toFixed(1)},${(before + dir * r).toFixed(1)} L${at.toFixed(1)},${(now - dir * r).toFixed(1)} Q${at.toFixed(1)},${now.toFixed(1)} ${(at + r).toFixed(1)},${now.toFixed(1)}`;
    },
    `M0,${level(0).toFixed(1)}`,
  )
  .concat(` L${W},${level(24).toFixed(1)}`);
const ALWAYS = y(PEAK_INSTANCES * PER);

// The gap between "always on" and what shpyrd runs: what you don't pay.
const saving = `${steps} L${W},${ALWAYS} L0,${ALWAYS} Z`;
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
const card = cn(glass, "rounded-2xl");

// The words on this slide, in three sizes and two greys (Giovani,
// 2026-10-28): captions 12 px in the muted grey (the legend, the moments,
// the readouts' and the Stat's labels); the cards' labels 14 px in the muted
// grey; values 14 px or larger, in the foreground, medium.
export function SleepDay() {
  const [apps, setApps] = useState(47);
  const [databases, setDatabases] = useState(true);
  const slider = useId();
  const [playing, setPlaying] = useState(true);

  // The hour shown: the day going by, or the one under the pointer.
  const [hour, setHour] = useState(START);
  const [pointer, setPointer] = useState<number | null>(null);
  const svg = useRef<SVGSVGElement>(null);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setPlaying(false);
      setHour(11);
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
  const n = instances(h);
  const awake = n > 0;

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
        <div className={cn(card, "grid gap-3 p-5")}>
          {/* The lines, in plain words. */}
          <ul className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
            <li className="flex items-center gap-1.5">
              <span className="h-2.5 w-3 rounded-sm bg-primary/25" />
              People using the app
            </li>
            <li className="flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded-full bg-foreground/70" />
              What shpyrd runs, and bills
            </li>
            <li className="flex items-center gap-1.5">
              <span className="w-3 border-t border-dashed border-destructive" />
              Always on, sized for the peak
            </li>
            <li className="flex items-center gap-1.5">
              <span className="h-2.5 w-3 rounded-sm bg-success/20" />
              What you don&apos;t pay
            </li>
          </ul>

          {/* The moments' labels above the chart, in one row (Giovani,
              2026-10-28): each beside its line rather than centred on it,
              the morning's to the left of theirs and the peak's to the right,
              so they don't meet; each line runs up to its label's corner. */}
          <div className="relative mt-[22px] h-11">
            {moments.map((m, i) => (
              <div
                key={m.text}
                className="pointer-events-none absolute inset-y-0"
                style={{ left: `${(x(m.at) / W) * 100}%` }}
              >
                <span
                  className={cn(
                    "absolute top-0 grid w-max max-w-[11rem] rounded-md bg-background px-2 py-1 text-xs leading-tight shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8",
                    i === 1 ? "-left-3" : "-right-3",
                  )}
                >
                  <span className="font-medium text-foreground tabular-nums">
                    {m.title}
                  </span>
                  <span className="text-muted-foreground">{m.text}</span>
                </span>
                <span className="absolute top-9 bottom-0 left-0 border-l-[1.5px] border-dashed border-primary/80" />
              </div>
            ))}
          </div>
          <div className="relative -mt-5">
            <svg
              ref={svg}
              viewBox={`0 0 ${W} ${H + 24}`}
              className="w-full cursor-crosshair overflow-visible"
              role="img"
              aria-label="A day of an internal app"
              onMouseMove={(e) => {
                const r = svg.current?.getBoundingClientRect();
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
              {[0, 6, 12, 18, 24].map((t) => (
                <g key={t}>
                  <line
                    x1={x(t)}
                    x2={x(t)}
                    y1={TOP}
                    y2={H}
                    className="stroke-foreground/10"
                    strokeDasharray="2 4"
                  />
                  <text
                    x={x(t)}
                    y={H + 18}
                    textAnchor={t === 0 ? "start" : t === 24 ? "end" : "middle"}
                    className="fill-muted-foreground text-[11px]"
                  >
                    {String(t).padStart(2, "0")}h
                  </text>
                </g>
              ))}
              {/* What you don't pay: between always on and what runs. */}
              <path d={saving} className="fill-success/10" />
              <line
                x1={0}
                x2={W}
                y1={ALWAYS}
                y2={ALWAYS}
                className="stroke-destructive"
                strokeDasharray="6 5"
                strokeWidth={1.5}
              />
              <defs>
                <linearGradient id="sleep-day-area" x1="0" y1="0" x2="0" y2="1">
                  <stop
                    offset="0"
                    className="[stop-color:var(--primary)]"
                    stopOpacity={0.3}
                  />
                  <stop
                    offset="0.9"
                    className="[stop-color:var(--primary)]"
                    stopOpacity={0}
                  />
                </linearGradient>
              </defs>
              <path d={area} fill="url(#sleep-day-area)" />
              <path
                d={`M${curve}`}
                className="fill-none stroke-primary"
                strokeWidth={2}
                strokeLinejoin="round"
              />
              <path
                d={steps}
                className="fill-none stroke-foreground/70"
                strokeWidth={2.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
              {/* The day's moments, as the home's metrics film marks its releases. */}
              {moments.map((m) => (
                <line
                  key={m.text}
                  x1={x(m.at)}
                  x2={x(m.at)}
                  y1={0}
                  y2={H}
                  className="stroke-primary/80"
                  strokeDasharray="4 3"
                  strokeWidth={1.5}
                />
              ))}
              {/* The hour shown. */}
              <g transform={`translate(${x(h).toFixed(2)} 0)`}>
                <line
                  x1={0}
                  x2={0}
                  y1={TOP}
                  y2={H}
                  className="stroke-foreground/40"
                />
                <circle
                  r={6}
                  cx={0}
                  cy={level(h)}
                  className={awake ? "fill-primary" : "fill-muted-foreground"}
                />
              </g>
            </svg>
            {/* What you don't pay, named where it is widest: the night. */}
            <span
              className="pointer-events-none absolute text-xs font-medium text-success"
              style={{
                left: `${(x(1) / W) * 100}%`,
                top: `${((ALWAYS + H) / 2 / (H + 24)) * 100}%`,
              }}
            >
              What you don&apos;t pay
            </span>
          </div>

          {/* The app at that hour, as the home's sleep film tells it. */}
          <dl className="grid grid-cols-4 gap-3 border-t border-border pt-4">
            <Readout label="Time">{clock(h)}</Readout>
            <Readout label="People using it">{Math.round(people(h))}</Readout>
            <Readout label="The app">
              <span
                className={cn(
                  "inline-flex items-center gap-1.5 [&_svg]:size-4",
                  awake ? "text-primary" : "text-muted-foreground",
                )}
              >
                {awake ? <Sun /> : <Moon />}
                {awake
                  ? `Awake · ${n} ${n === 1 ? "copy" : "copies"}`
                  : "Asleep"}
              </span>
            </Readout>
            <Readout label="Costing now">
              <span className={awake ? "text-foreground" : "text-success"}>
                ${(n * HOURLY).toFixed(2)}/h
              </span>
            </Readout>
          </dl>
        </div>

        {/* The month, for all the company's apps. */}
        <div className={cn(card, "grid content-start gap-5 p-5")}>
          {/* How many apps: the design system's Slider (Giovani,
              2026-10-28), the number on its knob. */}
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
          <div className="grid gap-4 border-t border-border pt-4">
            <div className="grid gap-1.5">
              <p className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">Always on</span>
                <span className="font-medium text-foreground tabular-nums">
                  {money(always)}/mo
                </span>
              </p>
              <ProgressBar value={100} tone="error" size="sm" />
            </div>
            <div className="grid gap-1.5">
              <p className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">On shpyrd</span>
                <span className="font-medium text-foreground tabular-nums">
                  {money(ours)}/mo
                </span>
              </p>
              <ProgressBar
                value={(ours / always) * 100}
                tone="success"
                size="sm"
              />
            </div>
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

function Readout({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1 rounded-2xl bg-muted/50 px-3 py-2 text-center">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-base font-medium tabular-nums">{children}</dd>
    </div>
  );
}
