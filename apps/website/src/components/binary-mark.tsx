"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

// A background of the shpyrd mark made of falling binary, in the manner of
// The Matrix's code rain: inside the mark, and only there, columns of 0s and
// 1s run down, each led by a bright digit and trailing off, over the mark's
// own dimmer digits. In the digits and inks of the home page's
// binary ocean (binary-ocean.tsx): Geist Mono, grey on the light page,
// orange on the dark one. Like the ocean it belongs to
// the page and scrolls with it: it spans from the top of the page to the
// bottom of the element marked [data-binary-end], and the mark stands from
// the hero's top over most of that height. Still, the mark whole, for who
// asked the system for less motion.

// The mark (design/ui brand.tsx), in its 140.71 × 140.64 box.
const MARK = [
  "M127.94,102.31c0,7.06-5.72,12.77-12.77,12.77h-25.65c-7.63,0-14.48,3.35-19.16,8.66-4.68-5.31-11.53-8.66-19.16-8.66h-25.64c-7.05,0-12.77-5.72-12.77-12.77v-16H0v16c0,14.11,11.44,25.55,25.55,25.55h25.64c7.05,0,12.77,5.72,12.77,12.77h12.77c0-7.06,5.72-12.77,12.77-12.77h25.64c14.11,0,25.55-11.44,25.55-25.55v-16h-12.77v16Z",
  "M132.87,38.37h0s-17.71,0-17.71,0v-12.77c7.05,0,12.77-5.72,12.77-12.77h0s-51.19,0-51.19,0V0h-12.77v12.82H12.77c0,7.06,5.72,12.77,12.77,12.77v12.77H7.84s-7.84,0-7.84,0v41.55h12.77v-28.78h115.16v28.78h12.77v-41.55h-7.84ZM38.32,25.6h64.06v12.77H38.32v-12.77Z",
  "M54.35,102.33L68.44,102.33L86.36,63.91L72.26,63.91Z",
  "M31.27,83.17L40.21,102.33L54.3,102.33L45.37,83.17L54.35,63.91L40.26,63.91Z",
  "M100.45,102.33L109.43,83.07L100.5,63.91L86.4,63.91L95.34,83.07L86.36,102.33Z",
].join("");

const CELL = 14;
const FPS = 30;
const themes = {
  // The home page's binary ocean's inks (binary-ocean.tsx): grey on the
  // light page, orange on the dark one.
  light: { mark: "110,115,124", markBase: 0.26 },
  dark: { mark: "255,122,61", markBase: 0.26 },
} as const;

export function BinaryMark({
  className,
  anchor,
  scale = 1,
  offset,
  shift = 0,
  height,
  x,
}: {
  className?: string;
  // The element the mark stands centred behind (else the middle of the
  // page's width, from the hero's top).
  anchor?: string;
  // How much larger than the space it is given (the hero's top to most of
  // the box) the mark is drawn.
  scale?: number;
  // Where the mark's top stands, in px from the top of the page (else it is
  // centred as above).
  offset?: number;
  // Moved this many px to the right of where it would stand.
  shift?: number;
  // A fixed size and place, the same on every page that uses them: the
  // mark's height in px, and its centre in px right of the page's middle.
  height?: number;
  x?: number;
}) {
  const ref = React.useRef<HTMLCanvasElement>(null);

  React.useEffect(() => {
    const canvas = ref.current;
    const c = canvas?.getContext("2d");
    if (!canvas || !c) return;
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let dark = document.documentElement.classList.contains("dark");
    const themed = new MutationObserver(() => {
      dark = document.documentElement.classList.contains("dark");
      if (still) paint(0);
    });
    themed.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });

    let dpr = 1;
    let cols = 0;
    let rows = 0;
    let inMark = new Uint8Array(0);
    let digits = new Uint8Array(0);
    // Each column's drop: where its head is (in rows), how fast it falls, how
    // long its tail is.
    let head = new Float32Array(0);
    let speed = new Float32Array(0);
    let tail = new Float32Array(0);

    const drop = (x: number, anywhere: boolean) => {
      head[x] = anywhere ? Math.random() * rows * 1.5 - rows * 0.5 : -Math.random() * rows * 0.6;
      speed[x] = 7 + Math.random() * 13;
      tail[x] = 8 + Math.random() * 18;
    };

    const resize = () => {
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      // Its box: the page's width, down to the bottom of the marked element.
      const box = canvas.getBoundingClientRect();
      const hero = document.querySelector("[data-slot=hero]")?.getBoundingClientRect();
      const end = document.querySelector("[data-binary-end]")?.getBoundingClientRect();
      const behind = anchor ? document.querySelector(anchor)?.getBoundingClientRect() : undefined;
      const h = Math.max(
        end ? end.bottom - box.top : window.innerHeight,
        height !== undefined ? (offset ?? 0) + height + CELL * 2 : 0,
        400,
      );
      canvas.style.height = `${h}px`;
      const w = canvas.clientWidth;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      cols = Math.ceil(w / CELL);
      rows = Math.ceil(h / CELL);

      // Where the mark stands: from the hero's top over most of the box, in
      // the middle of the page's width; drawn once on a grid of cells, a cell
      // in it when its centre is.
      const top = hero ? hero.top - box.top : 0;
      const tall = (h - top) * 0.86;
      const grid = document.createElement("canvas");
      grid.width = cols;
      grid.height = rows;
      const g = grid.getContext("2d")!;
      const size =
        height !== undefined
          ? Math.min(height / CELL / 140.64, (cols * 0.7) / 140.71)
          : Math.min(tall / CELL / 140.64, (cols * 0.7) / 140.71) * scale;
      // Its centre: the anchor's, or the middle of the space it is given.
      const cx =
        x !== undefined
          ? Math.min(cols / 2 + x / CELL, cols - (140.71 * size) / 2)
          : (behind ? (behind.left + behind.width / 2 - box.left) / CELL : cols / 2) + shift / CELL;
      const cy =
        offset !== undefined
          ? offset / CELL + (140.64 * size) / 2
          : behind
            ? (behind.top + behind.height / 2 - box.top) / CELL
            : top / CELL + tall / CELL / 2;
      g.translate(cx - (140.71 * size) / 2, cy - (140.64 * size) / 2);
      g.scale(size, size);
      g.fill(new Path2D(MARK));
      const px = g.getImageData(0, 0, cols, rows).data;
      inMark = new Uint8Array(cols * rows);
      for (let i = 0; i < cols * rows; i++) inMark[i] = px[i * 4 + 3] > 110 ? 1 : 0;

      digits = new Uint8Array(cols * rows);
      for (let i = 0; i < digits.length; i++) digits[i] = Math.random() < 0.5 ? 0 : 1;
      head = new Float32Array(cols);
      speed = new Float32Array(cols);
      tail = new Float32Array(cols);
      for (let x = 0; x < cols; x++) drop(x, true);
      paint(0);
    };

    function paint(dt: number) {
      if (!c) return;
      const t = dark ? themes.dark : themes.light;
      c.setTransform(dpr, 0, 0, dpr, 0, 0);
      c.clearRect(0, 0, canvas!.width, canvas!.height);
      c.font = `500 ${CELL - 3}px "Geist Mono Variable", ui-monospace, monospace`;
      c.textAlign = "center";
      c.textBaseline = "middle";

      // A few digits change as the rain runs.
      for (let k = 0; k < digits.length * 0.004; k++) {
        const i = Math.floor(Math.random() * digits.length);
        digits[i] ^= 1;
      }

      for (let x = 0; x < cols; x++) {
        if (!still) {
          head[x] += speed[x] * dt;
          if (head[x] - tail[x] > rows) drop(x, false);
        }
        for (let y = 0; y < rows; y++) {
          const i = y * cols + x;
          // How lit the rain leaves this cell: the head brightest, the tail
          // fading behind it, nothing ahead of it.
          const behind = still ? -1 : head[x] - y;
          const rain = behind >= 0 && behind < tail[x] ? 1 - behind / tail[x] : 0;
          // Only the mark: outside it, nothing falls.
          if (!inMark[i]) continue;
          // The rain only lifts the mark a little: it is felt more than seen.
          const a = t.markBase + (1 - t.markBase) * rain * (behind < 1 ? 0.4 : 0.3);
          c.fillStyle = `rgba(${t.mark},${a.toFixed(3)})`;
          c.fillText(digits[i] ? "1" : "0", x * CELL + CELL / 2, y * CELL + CELL / 2);
        }
      }
    }

    resize();
    window.addEventListener("resize", resize);
    const end = document.querySelector("[data-binary-end]");
    const sized = new ResizeObserver(() => resize());
    if (end) sized.observe(end);
    document.fonts?.load(`500 11px "Geist Mono Variable"`).then(() => paint(0), () => {});

    let frame = 0;
    let last = performance.now();
    let acc = 0;
    const tick = (now: number) => {
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      acc += dt;
      if (acc >= 1 / FPS) {
        paint(acc);
        acc = 0;
      }
      frame = requestAnimationFrame(tick);
    };
    if (!still) frame = requestAnimationFrame(tick);

    return () => {
      cancelAnimationFrame(frame);
      themed.disconnect();
      sized.disconnect();
      window.removeEventListener("resize", resize);
    };
  }, [anchor, scale, offset, shift, height, x]);

  return (
    <canvas
      ref={ref}
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-x-0 top-0 -z-10 w-full", className)}
    />
  );
}
