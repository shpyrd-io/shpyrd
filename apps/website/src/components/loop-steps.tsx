"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

type Step = { icon: React.ReactElement; heading: string; body: string };

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
  className,
}: {
  steps: Step[];
  // The words on the way back.
  again?: string;
  className?: string;
}) {
  const box = React.useRef<HTMLDivElement>(null);
  const [track, setTrack] = React.useState<{ d: string; w: number; h: number; wide: boolean; label: [number, number] } | null>(null);

  React.useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
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
        d = `M ${first.x} ${y} H ${outR - r} q ${r} 0 ${r} ${r} V ${foot - r} q 0 ${r} ${-r} ${r} H ${outL + r} q ${-r} 0 ${-r} ${-r} V ${y + r} q 0 ${-r} ${r} ${-r} Z`;
        label = [(first.x + last.x) / 2, foot];
      } else {
        // One under the other: forward down the tiles, back up their left.
        const x = first.x;
        const side = 6;
        const r = Math.min(20, (x - side) / 1.2);
        d = `M ${x} ${first.y} V ${last.y} q 0 ${r} ${-r} ${r} H ${side + r} q ${-r} 0 ${-r} ${-r} V ${first.y - r} q 0 ${-r} ${r} ${-r} H ${x - r} q ${r} 0 ${r} ${r} Z`;
        label = [side, (first.y + last.y) / 2];
      }
      setTrack({ d, w: b.width, h: b.height, wide, label });
      // Room under the words for the foot of the track and its label.
      const list = el.querySelector<HTMLElement>("ol");
      const room = wide ? `${Math.round(label[1] - words + 14)}px` : "";
      if (list && list.style.paddingBottom !== room) list.style.paddingBottom = room;
    };
    draw();
    const watch = new ResizeObserver(draw);
    watch.observe(el);
    return () => watch.disconnect();
  }, [steps.length]);

  const [still, setStill] = React.useState(false);
  React.useEffect(() => setStill(window.matchMedia("(prefers-reduced-motion: reduce)").matches), []);

  return (
    <div ref={box} className={cn("@container/loop relative isolate", className)}>
      {track && (
        <svg aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 overflow-visible" width={track.w} height={track.h}>
          <path id="loop-track" d={track.d} fill="none" className="stroke-foreground/40 dark:stroke-foreground/50" strokeWidth="1.75" strokeDasharray="4 6" />
          {/* The light going round. */}
          {!still && (
            <circle r="4" className="fill-primary">
              <animateMotion dur="9s" repeatCount="indefinite" rotate="auto">
                <mpath href="#loop-track" />
              </animateMotion>
            </circle>
          )}
        </svg>
      )}

      {/* The words in a soft glow of the page's own colour (white on the
          light page, black on the dark), so they read over the moving
          binary behind them. */}
      <ol className="grid gap-10 page-glow pb-20 pl-14 @3xl/loop:grid-cols-3 @3xl/loop:gap-8 @3xl/loop:pb-24 @3xl/loop:pl-0">
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
            "absolute inline-flex items-center gap-1.5 bg-background px-3 text-xs font-medium text-primary page-glow",
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
