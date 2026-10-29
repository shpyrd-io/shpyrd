"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

// What the feature cards' films share (Giovani, 2026-10-19): each is a
// square on its card with a window floating in it, the window in the glass
// of the hero's, with room all round and a shadow short enough to end in that
// room (the square clips it). A film is a list of moments, each held for its
// time; it plays only while some of it is on the screen, starts again at the
// end, and for who asked for less motion stays on its last moment.

export function useFilm<M extends string>(moments: readonly (readonly [M, number])[]) {
  const last = moments.length - 1;
  const [moment, setMoment] = useState(0);
  const [round, setRound] = useState(0);
  const [still, setStill] = useState<boolean | null>(null);
  const [seen, setSeen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const quiet = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setStill(quiet);
    if (quiet) setMoment(last);
  }, [last]);

  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const sight = new IntersectionObserver(([e]) => setSeen(e.isIntersecting));
    sight.observe(el);
    return () => sight.disconnect();
  }, []);

  useEffect(() => {
    if (still !== false || !seen) return;
    const timer = window.setTimeout(() => {
      if (moment < last) return setMoment(moment + 1);
      setRound((r) => r + 1);
      setMoment(0);
    }, moments[moment][1]);
    return () => window.clearTimeout(timer);
  }, [moment, still, seen, last, moments]);

  const name = moments[moment][0];
  const index = (m: M) => moments.findIndex(([n]) => n === m);
  const past = (m: M) => moment >= index(m);
  return { root, name, past, round, still: still === true };
}

// The square's window: floating in the glass, 20px from the square's edges.
export function FilmFrame({
  root,
  children,
}: {
  root: React.RefObject<HTMLDivElement | null>;
  children: React.ReactNode;
}) {
  return (
    <div ref={root} className="absolute inset-0 p-5 font-sans">
      <div
        className={cn(
          glass,
          "size-full rounded-2xl p-2 shadow-[0_8px_16px_-10px_rgb(20_20_30/0.22),0_2px_4px_-2px_rgb(20_20_30/0.08)]",
        )}
      >
        {children}
      </div>
    </div>
  );
}

// The window inside the frame: the hero's, round 10px, edged in white, its
// bar on its ground. Inside the films `muted` is a colour of text, so its
// ground fades to the page's ink, thinly.
export const filmWindow =
  "h-full rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-foreground/[0.03] shadow-none dark:border-white/10 dark:from-card dark:to-background";

// The size of a box in pixels, kept as it changes: for lines drawn between
// pieces laid out by the page, so they meet them whatever the card's size.
// A ref given as a function, so a box drawn again (a film's next round) is
// measured again: a box taken off the page measures nothing, and its lines
// would go with it.
export function useSize<E extends Element>() {
  const [el, setEl] = useState<E | null>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  useEffect(() => {
    if (!el) return;
    const seen = new ResizeObserver(([e]) => {
      const { width, height } = e.contentRect;
      if (width > 0 && height > 0) setSize({ w: width, h: height });
    });
    seen.observe(el);
    return () => seen.disconnect();
  }, [el]);
  return [setEl, size] as const;
}

// Every film sits 20px inside its stage, on all sides, and fills the rest.
export const filmInset = "absolute inset-0 p-5 font-sans text-[11px]";

// A piece floating on the card, with no window: the glass of the films'
// windows, round 12px, its shadow short.
export const filmPiece = cn(
  glass,
  "rounded-2xl shadow-[0_6px_12px_-8px_rgb(20_20_30/0.25),0_1px_3px_-1px_rgb(20_20_30/0.08)]",
);

// How what comes in a film comes in.
export const filmEnter = "animate-in fade-in-0 slide-in-from-bottom-1 duration-normal ease-enter";

// A pulse up one of a film's lines, once, as the light on the site's lines:
// lengths of orange laid one over another, centred together, the shorter the
// brighter, so it fades at both ends, in a soft glow; from the line's start
// to past its end (@keyframes film-pulse, src/styles/feature-blocks.css).
// Given a key that changes, it runs again; with `every`, it runs again and
// again, a pulse that often (ms), from `after` (ms).
export function FilmPulse({ d, every, after = 0 }: { d: string; every?: number; after?: number }) {
  return (
    <g style={{ filter: "drop-shadow(0 0 4px rgb(255 79 0 / 0.55))" }}>
      {[
        { seg: 34, opacity: 0.12 },
        { seg: 24, opacity: 0.2 },
        { seg: 15, opacity: 0.32 },
        { seg: 8, opacity: 0.5 },
        { seg: 4, opacity: 0.85 },
      ].map(({ seg, opacity }) => (
        <path
          key={seg}
          d={d}
          fill="none"
          pathLength={100}
          strokeWidth={1.5}
          strokeOpacity={opacity}
          strokeDasharray={`${seg} 300`}
          className="stroke-primary"
          style={{
            animation: every
              ? `film-pulse ${every}ms ease-in-out ${after}ms infinite both`
              : "film-pulse 850ms ease-in-out forwards",
            ["--from" as string]: `${seg / 2 + 20}`,
            ["--to" as string]: `${seg / 2 - 120}`,
          }}
        />
      ))}
    </g>
  );
}

// The part of a text typed so far, a letter every so often from when it is
// asked for; for a field's value, where the text must be a string.
export function useTyped(text: string, on: boolean, every = 70) {
  const [shown, setShown] = useState(0);
  useEffect(() => {
    setShown(0);
    if (!on) return;
    const timer = window.setInterval(() => setShown((n) => (n >= text.length ? n : n + 1)), every);
    return () => window.clearInterval(timer);
  }, [text, on, every]);
  return text.slice(0, shown);
}

// Typed a letter at a time, from when it is first shown.
export function Typed({ text, every = 70 }: { text: string; every?: number }) {
  const [shown, setShown] = useState(0);
  useEffect(() => {
    setShown(0);
    const timer = window.setInterval(() => setShown((n) => (n >= text.length ? n : n + 1)), every);
    return () => window.clearInterval(timer);
  }, [text, every]);
  return (
    <>
      {text.slice(0, shown)}
      <span aria-hidden="true" className="ml-px inline-block h-[1em] w-px translate-y-[0.15em] animate-pulse bg-foreground" />
    </>
  );
}
