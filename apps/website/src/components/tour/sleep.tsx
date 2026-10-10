"use client";

// Slide 7: an internal app's day, as the original draws it. People come at
// nine and leave at six; the app wakes with the first of them, adds an
// instance for every eight people online, gives them back a quarter of an
// hour after the load drops, and sleeps when nobody is left. Against it,
// what an app always on and sized for the peak is paid for. Beside it, what
// that does to the bill of a company's apps.
import { useEffect, useId, useRef, useState } from "react";
import { ChartNoAxesColumnIncreasing } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Switch } from "@shpyrd/ui/components/switch";
import { sleep } from "@shpyrd/content/site/tour";
import { Heading, Label, Lead, panel } from "./parts";

// People online at an hour of the day (8.5 is half past eight).
function people(h: number) {
  const bump = (centre: number, width: number, height: number) => height * Math.exp(-(((h - centre) / width) ** 2));
  return h >= 8.5 && h <= 18.5 ? bump(11, 2.2, 34) + bump(15.5, 2, 28) : 0;
}
// An instance for every eight people, kept for a quarter of an hour after
// the load drops; none at all once nobody has been there for that long.
const PER = 8;
function instances(h: number) {
  let most = 0;
  for (let back = 0; back <= 0.25; back += 0.05) most = Math.max(most, Math.ceil(people(h - back) / PER));
  return most;
}
// The day, every three minutes: enough for a smooth curve.
const samples = Array.from({ length: 481 }, (_, i) => i / 20);
const PEAK_INSTANCES = Math.max(...samples.map(instances));
const instanceShare = samples.reduce((n, h) => n + instances(h), 0) / samples.length / PEAK_INSTANCES;
// An hour of the day as a clock says it: 8.5 is 08:30.
const clock = (h: number) => `${String(Math.floor(h)).padStart(2, "0")}:${String(Math.floor((h % 1) * 60)).padStart(2, "0")}`;
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
const curve = samples.map((h) => `${x(h).toFixed(1)},${y(people(h)).toFixed(1)}`).join(" L");
const area = `M0,${H} L${curve} L${W},${H} Z`;
const level = (h: number) => y(instances(h) * PER);
const steps = samples
  .map((h, i) => {
    if (i === 0) return `M0,${level(h).toFixed(1)}`;
    const before = level(samples[i - 1]);
    const now = level(h);
    return now === before ? "" : `L${x(h).toFixed(1)},${before.toFixed(1)} L${x(h).toFixed(1)},${now.toFixed(1)}`;
  })
  .join(" ")
  .concat(` L${W},${level(24).toFixed(1)}`);
const ALWAYS = y(PEAK_INSTANCES * PER);

export function Sleep() {
  const [apps, setApps] = useState(47);
  const [databases, setDatabases] = useState(true);
  const slider = useId();

  // The day goes by frame by frame and starts again. The cursor and the
  // readouts are moved where they are, not drawn again by React, so nothing
  // else on the slide is touched sixty times a second. Still at eleven in
  // the morning for who asked for less motion.
  const cursor = useRef<SVGGElement>(null);
  const dot = useRef<SVGCircleElement>(null);
  const time = useRef<HTMLElement>(null);
  const online = useRef<HTMLElement>(null);
  const running = useRef<HTMLElement>(null);
  useEffect(() => {
    const show = (h: number) => {
      const n = instances(h);
      cursor.current?.setAttribute("transform", `translate(${x(h).toFixed(2)} 0)`);
      dot.current?.setAttribute("cy", level(h).toFixed(1));
      dot.current?.setAttribute("class", n ? "fill-success" : "fill-muted-foreground");
      if (time.current) time.current.textContent = clock(h);
      if (online.current) online.current.textContent = String(Math.round(people(h)));
      if (running.current) {
        running.current.textContent = n ? String(n) : sleep.asleep;
        running.current.dataset.up = n ? "true" : "false";
      }
    };
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return show(11);
    let frame = 0;
    const start = performance.now() - (START / 24) * DAY_MS;
    const tick = (t: number) => {
      show((((t - start) % DAY_MS) / DAY_MS) * 24);
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, []);

  const always = apps * (APP + DATABASE);
  const ours = apps * (APP * instanceShare + (databases ? DATABASE * instanceShare + DISK : DATABASE));
  const saved = Math.round((1 - ours / always) * 100);

  return (
    <div className="grid gap-8">
      <div className="grid gap-4">
        <Label icon={<ChartNoAxesColumnIncreasing />}>{sleep.label}</Label>
        <Heading>
          {sleep.heading[0]} <span className="text-primary">{sleep.heading[1]}</span>
        </Heading>
        <Lead>{sleep.lead}</Lead>
      </div>

      <div className="grid gap-5 lg:grid-cols-[1fr_20rem]">
        <div className={cn(panel, "grid gap-4 p-5")}>
          <figure className="grid gap-3">
            <figcaption className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <span className="h-2.5 w-3 rounded-sm bg-info/30" />
                {sleep.legend.users}
              </span>
              <span className="flex items-center gap-1.5">
                <span className="h-0.5 w-3 bg-success" />
                {sleep.legend.instances}
              </span>
              <span className="flex items-center gap-1.5">
                <span className="w-3 border-t border-dashed border-destructive" />
                {sleep.legend.always}
              </span>
            </figcaption>
            <svg viewBox={`0 0 ${W} ${H + 24}`} className="w-full overflow-visible" role="img" aria-label={sleep.legend.instances}>
              {[0, 6, 12, 18, 24].map((h) => (
                <g key={h}>
                  <line x1={x(h)} x2={x(h)} y1={TOP} y2={H} className="stroke-border" strokeDasharray="2 4" />
                  <text
                    x={x(h)}
                    y={H + 18}
                    textAnchor={h === 0 ? "start" : h === 24 ? "end" : "middle"}
                    className="fill-muted-foreground font-mono text-[11px]"
                  >
                    {String(h).padStart(2, "0")}h
                  </text>
                </g>
              ))}
              <rect x={0} y={ALWAYS} width={W} height={H - ALWAYS} className="fill-destructive/5" />
              <line x1={0} x2={W} y1={ALWAYS} y2={ALWAYS} className="stroke-destructive" strokeDasharray="6 5" strokeWidth={1.5} />
              <path d={area} className="fill-info/20" />
              <path d={`M${curve}`} className="fill-none stroke-info" strokeWidth={1.5} strokeLinejoin="round" />
              <path d={steps} className="fill-none stroke-success" strokeWidth={3} strokeLinejoin="round" />
              {/* The time: a line down the day, and a dot on the steps. */}
              <g ref={cursor} transform={`translate(${x(START)} 0)`}>
                <line x1={0} x2={0} y1={TOP} y2={H} className="stroke-foreground/40" />
                <circle ref={dot} r={6} cx={0} cy={level(START)} className="fill-muted-foreground" />
              </g>
            </svg>
          </figure>

          {/* Where the day is: the time, who is online, what runs. */}
          <dl className="grid grid-cols-3 gap-3 border-t border-border pt-4">
            <Readout label={sleep.hour}>
              <span ref={time}>{clock(START)}</span>
            </Readout>
            <Readout label={sleep.online}>
              <span ref={online}>0</span>
            </Readout>
            <Readout label={sleep.state}>
              <span ref={running} data-up="false" className="text-muted-foreground data-[up=true]:text-success">
                {sleep.asleep}
              </span>
            </Readout>
          </dl>
        </div>

        <div className={cn(panel, "grid content-start gap-5 p-5")}>
          <div className="grid gap-2">
            <label htmlFor={slider} className="flex items-center justify-between text-sm">
              {sleep.apps}
              <span className="font-mono tabular-nums">{apps}</span>
            </label>
            <input
              id={slider}
              type="range"
              min={5}
              max={150}
              value={apps}
              onChange={(e) => setApps(Number(e.target.value))}
              className="w-full accent-primary"
            />
          </div>
          <label className="flex items-center justify-between gap-3 text-sm">
            {sleep.databases}
            <Switch checked={databases} onCheckedChange={setDatabases} statusLabel={false} />
          </label>
          <div className="grid gap-3 border-t border-border pt-4">
            <Bar label={sleep.alwaysOn} value={always} of={always} tone="bg-destructive" />
            <Bar label={sleep.onShpyrd} value={ours} of={always} tone="bg-success" />
          </div>
          <div>
            <p className="font-heading text-5xl font-semibold text-primary tabular-nums">−{saved}%</p>
            <p className="text-xs text-muted-foreground">
              {sleep.saving} · {sleep.note}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

function Bar({ label, value, of, tone }: { label: string; value: number; of: number; tone: string }) {
  return (
    <div className="grid gap-1.5">
      <p className="flex items-center justify-between text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono tabular-nums">{money(value)}/mo</span>
      </p>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full transition-[width] duration-slow ease-move", tone)} style={{ width: `${(value / of) * 100}%` }} />
      </div>
    </div>
  );
}

function Readout({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1 rounded-xl bg-muted/50 px-3 py-2 text-center">
      <dt className="text-[0.6875rem] tracking-[0.12em] text-muted-foreground uppercase">{label}</dt>
      <dd className="font-mono text-lg tabular-nums">{children}</dd>
    </div>
  );
}
