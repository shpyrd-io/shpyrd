"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

type Step = { icon: React.ReactElement; heading: string; body: React.ReactNode };

// Steps that come round again: a loop, drawn as one. The steps stand on a
// track in the shape of a stadium, numbered, the way forward along its top
// and the way back along its bottom, from the last step to the first, with a
// word on it saying the loop starts again. A light runs round the track, on
// and on, as the work does; the track drawn firm enough to read over the
// binary behind it, and round the outside of the steps' words; still, the track whole, for who asked the system
// for less motion. On a phone the steps stand one under the other and the way
// back runs up their left side.
export function LoopSteps({
  steps,
  again = "And again, for the next change",
  back = 0,
  light = "glow",
  className,
}: {
  steps: Step[];
  // The words on the way back.
  again?: string;
  // The step the way back returns to (from 0). Steps before it are done once:
  // the track leads from them into the loop, and the light goes round only
  // the loop (the home's "From your laptop to your team": connect once, then
  // deploy, share and change, again and again).
  back?: number;
  // What goes round: an orange reflection along a continuous grey line,
  // faded at both ends, with a soft glow (Giovani, 2026-10-19: the site's
  // loops all draw it), or the dot on a dashed track it replaced.
  light?: "dot" | "glow";
  className?: string;
}) {
  const glow = light === "glow";
  const box = React.useRef<HTMLDivElement>(null);
  const [track, setTrack] = React.useState<{
    d: string;
    lead?: string;
    w: number;
    h: number;
    wide: boolean;
    label: [number, number];
    dur: number;
    len: number;
    twice: string;
  } | null>(null);

  React.useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    let lead: string | undefined;
    const setLead = (v: string | undefined) => (lead = v);
    const draw = () => {
      const b = el.getBoundingClientRect();
      const tiles = [...el.querySelectorAll<HTMLElement>("[data-loop-tile]")].map((t) => {
        const r = t.getBoundingClientRect();
        return { x: r.left - b.left + r.width / 2, y: r.top - b.top + r.height / 2, h: r.height };
      });
      if (tiles.length < 2) return;
      // The steps' own edges, words and all, so the way round passes outside them.
      const items = [...el.querySelectorAll<HTMLElement>("ol > li")].map((li) => li.getBoundingClientRect());
      const left = Math.min(...items.map((r) => r.left)) - b.left;
      const right = Math.max(...items.map((r) => r.right)) - b.left;
      const words = Math.max(...items.map((r) => r.bottom)) - b.top;
      // One gap all round: as far as the track's top runs from the steps'
      // titles, it runs from their words at the sides and at the foot.
      const title = el.querySelector("ol > li h3")?.getBoundingClientRect();
      const first = tiles[0];
      const last = tiles[tiles.length - 1];
      const wide = Math.abs(first.y - last.y) < 4;
      let d: string;
      let label: [number, number];
      if (wide) {
        // Forward along the tiles' centres; down outside the last step's
        // words, back along the foot of the box, up outside the first's, so
        // the light never runs over the words.
        const y = first.y;
        const gap = title ? title.top - b.top - y : 24;
        const foot = words + gap;
        const r = Math.min(48, (foot - y) / 2);
        const outL = left - gap;
        const outR = right + gap;
        if (back > 0 && back < tiles.length) {
          // The loop closes between the step before `back` and `back` itself,
          // in the room between their words; the steps before lead into it.
          const gx = (items[back - 1].right + items[back].left) / 2 - b.left;
          // Its corner where it rejoins the top is round, as the others are.
          d = `M ${gx + r} ${y} H ${outR - r} q ${r} 0 ${r} ${r} V ${foot - r} q 0 ${r} ${-r} ${r} H ${gx + r} q ${-r} 0 ${-r} ${-r} V ${y + r} q 0 ${-r} ${r} ${-r} Z`;
          setLead(`M ${first.x} ${y} H ${gx + r}`);
          label = [(tiles[back].x + last.x) / 2, foot];
        } else {
          d = `M ${first.x} ${y} H ${outR - r} q ${r} 0 ${r} ${r} V ${foot - r} q 0 ${r} ${-r} ${r} H ${outL + r} q ${-r} 0 ${-r} ${-r} V ${y + r} q 0 ${-r} ${r} ${-r} Z`;
          setLead(undefined);
          label = [(first.x + last.x) / 2, foot];
        }
      } else {
        // One under the other: forward down the tiles, back up their left.
        const x = first.x;
        const side = 6;
        const r = Math.min(20, (x - side) / 1.2);
        d = `M ${x} ${first.y} V ${last.y} q 0 ${r} ${-r} ${r} H ${side + r} q ${-r} 0 ${-r} ${-r} V ${first.y - r} q 0 ${-r} ${r} ${-r} H ${x - r} q ${r} 0 ${r} ${r} Z`;
        label = [side, (first.y + last.y) / 2];
        setLead(undefined);
      }
      // The light at one even pace whatever the track's length (about 160px
      // a second), so a longer loop is not a faster one.
      const probe = document.createElementNS("http://www.w3.org/2000/svg", "path");
      probe.setAttribute("d", d);
      const len = probe.getTotalLength();
      // Measured before the page is laid out, the track is no shape yet.
      if (!Number.isFinite(len) || len <= 0) return;
      const dur = Math.max(9, Math.round(len / 160));
      // The reflection runs on the loop drawn twice in one stroke, so it never
      // meets the stroke's start or end (where a dash is cut): it travels one
      // round in the middle of the two and starts again where it began.
      const [, sx, sy] = d.match(/^M\s*([-\d.e]+)\s+([-\d.e]+)/) ?? [];
      const body = d.replace(/^M\s*[-\d.e]+\s+[-\d.e]+/, "").replace(/Z\s*$/, `L ${sx} ${sy}`);
      const twice = `M ${sx} ${sy}${body}${body}`;

      setTrack({ d, twice, lead, w: b.width, h: b.height, wide, label, dur, len });
      // Room under the words for the foot of the track and its label.
      const list = el.querySelector<HTMLElement>("ol");
      const room = wide ? `${Math.round(label[1] - words + 14)}px` : "";
      if (list && list.style.paddingBottom !== room) list.style.paddingBottom = room;
    };
    draw();
    const watch = new ResizeObserver(draw);
    watch.observe(el);
    return () => watch.disconnect();
  }, [steps.length, back]);

  const [still, setStill] = React.useState(false);
  React.useEffect(() => setStill(window.matchMedia("(prefers-reduced-motion: reduce)").matches), []);

  return (
    // It comes into view whole (scroll-reveal.tsx): its track is drawn from
    // where its steps are, so they must not move on their own.
    <div ref={box} data-reveal-item="" data-reveal-whole="" className={cn("@container/loop relative isolate", className)}>
      {track && (
        <svg aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 overflow-visible" width={track.w} height={track.h}>
          {/* The track: dashed, or (light="glow") one continuous grey line. */}
          <path
            id="loop-track"
            d={track.d}
            fill="none"
            className={glow ? "stroke-foreground/12 dark:stroke-foreground/20" : "stroke-foreground/40 dark:stroke-foreground/50"}
            strokeWidth={glow ? 1.5 : 1.75}
            strokeDasharray={glow ? undefined : "4 6"}
          />
          {/* The steps done once, leading into the loop. */}
          {track.lead && (
            <path
              d={track.lead}
              fill="none"
              className={glow ? "stroke-foreground/12 dark:stroke-foreground/20" : "stroke-foreground/40 dark:stroke-foreground/50"}
              strokeWidth={glow ? 1.5 : 1.75}
              strokeDasharray={glow ? undefined : "4 6"}
            />
          )}
          {/* The light going round: a dot, or (light="glow") a reflection of
              orange running along the line. */}
          {!still && !glow && (
            <circle r="4" className="fill-primary">
              <animateMotion dur={`${track.dur}s`} repeatCount="indefinite" calcMode="paced">
                <mpath href="#loop-track" />
              </animateMotion>
            </circle>
          )}
          {!still && glow && (
            // A reflection of orange along the line, as thin as the line:
            // lengths of it laid one over another, centred together, the
            // shorter the brighter, so it fades out at both ends; a soft glow
            // round them, of its own orange, one for all the lengths (a glow
            // costs a lot to draw again each frame).
            <g style={{ filter: "drop-shadow(0 0 4px rgb(255 79 0 / 0.55))" }}>
              {[
              { seg: 240, opacity: 0.12 },
              { seg: 180, opacity: 0.18 },
              { seg: 120, opacity: 0.28 },
              { seg: 70, opacity: 0.45 },
              { seg: 30, opacity: 0.8 },
            ].map(({ seg, opacity }) => (
              <path
                key={seg}
                d={track.twice}
                fill="none"
                className="stroke-primary"
                strokeWidth={1.5}
                strokeOpacity={opacity}
                strokeDasharray={`${seg} ${2 * track.len}`}
              >
                {/* Its middle from 240px into the doubled loop to one round
                    further: never near either end of the stroke. */}
                <animate
                  attributeName="stroke-dashoffset"
                  from={seg / 2 - 240}
                  to={seg / 2 - 240 - track.len}
                  dur={`${track.dur}s`}
                  repeatCount="indefinite"
                />
              </path>
              ))}
            </g>
          )}
        </svg>
      )}

      {/* The words in a soft glow of the page's own colour (white on the
          light page, black on the dark), so they read over the moving
          binary behind them. */}
      <ol
        className={cn(
          "grid gap-10 page-glow pb-20 pl-14 @3xl/loop:gap-8 @3xl/loop:pb-24 @3xl/loop:pl-0",
          steps.length === 4 ? "@3xl/loop:grid-cols-4" : "@3xl/loop:grid-cols-3",
        )}
      >
        {steps.map((s, i) => (
          <li key={s.heading} className="grid content-start justify-items-start gap-3 @3xl/loop:justify-items-center @3xl/loop:text-center">
            <span
              data-loop-tile
              className={cn(
                "relative flex size-14 items-center justify-center rounded-2xl bg-background text-primary ring-1 ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card [&_svg]:size-6",
              )}
            >
              {s.icon}
              <span className="absolute -top-2 -right-2 grid size-5 place-items-center rounded-full bg-primary font-mono text-[11px] font-semibold text-primary-foreground [text-shadow:none]">
                {i + 1}
              </span>
            </span>
            <h3 className="mt-1 font-heading text-base font-semibold text-foreground">{s.heading}</h3>
            <p className="max-w-[34ch] text-sm text-muted-foreground">{s.body}</p>
          </li>
        ))}
      </ol>

      {track && (
        // The word on the way back, on the track, the page's colour behind it.
        <span
          className={cn(
            "absolute inline-flex items-center gap-1.5 bg-(--page-ground) px-3 text-xs font-medium text-primary page-glow",
            track.wide ? "-translate-x-1/2 -translate-y-1/2" : "-translate-x-1/2 -translate-y-1/2 -rotate-90 whitespace-nowrap",
          )}
          style={{ left: track.label[0], top: track.label[1] }}
        >
          ↻ {again}
        </span>
      )}
    </div>
  );
}
