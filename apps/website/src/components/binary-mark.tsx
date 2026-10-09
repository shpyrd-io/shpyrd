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
  centred,
  near = 0,
  tilt = false,
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
  // Where it stands instead when the page's hero is centred: in the middle of
  // the page's width, this tall, its top this many px from the page's top.
  centred?: { height: number; offset: number };
  // Rain around the mark too, softly, only this many cells out from it,
  // fainter the farther.
  near?: number;
  // The mark leans softly toward the pointer, in 3D.
  tilt?: boolean;
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
    // How near the mark each cell outside it is: 1 next to it, 0 at \`near\`
    // cells out or more.
    let around = new Float32Array(0);
    let pivot: [number, number] = [0, 0];
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
      const heroEl = document.querySelector<HTMLElement>("[data-slot=hero]");
      const hero = heroEl?.getBoundingClientRect();
      const end = document.querySelector("[data-binary-end]")?.getBoundingClientRect();
      const behind = anchor ? document.querySelector(anchor)?.getBoundingClientRect() : undefined;
      // Under a centred hero, the mark stands centred too, at its own size and place.
      const middle = centred && heroEl?.dataset.align === "center" ? centred : undefined;
      const markHeight = middle ? middle.height : height;
      const markOffset = middle ? middle.offset : offset;
      const markX = middle ? 0 : x;
      const h = Math.max(
        end ? end.bottom - box.top : window.innerHeight,
        markHeight !== undefined ? (markOffset ?? 0) + markHeight + CELL * 2 : 0,
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
      // Centred, it may take the page's whole width; beside the words, 70% of it.
      const room = cols * (middle ? 1 : 0.7);
      const size =
        markHeight !== undefined
          ? Math.min(markHeight / CELL / 140.64, room / 140.71)
          : Math.min(tall / CELL / 140.64, room / 140.71) * scale;
      // Its centre: the anchor's, or the middle of the space it is given.
      const cx =
        markX !== undefined
          ? Math.min(cols / 2 + markX / CELL, cols - (140.71 * size) / 2)
          : (behind ? (behind.left + behind.width / 2 - box.left) / CELL : cols / 2) + shift / CELL;
      const cy =
        markOffset !== undefined
          ? markOffset / CELL + (140.64 * size) / 2
          : behind
            ? (behind.top + behind.height / 2 - box.top) / CELL
            : top / CELL + tall / CELL / 2;
      g.translate(cx - (140.71 * size) / 2, cy - (140.64 * size) / 2);
      g.scale(size, size);
      g.fill(new Path2D(MARK));
      const px = g.getImageData(0, 0, cols, rows).data;
      inMark = new Uint8Array(cols * rows);
      for (let i = 0; i < cols * rows; i++) inMark[i] = px[i * 4 + 3] > 110 ? 1 : 0;
      // The distance of each cell from the mark, in cells, grown outward a
      // ring at a time.
      around = new Float32Array(cols * rows);
      if (near > 0) {
        let ring = Array.from(inMark.keys()).filter((i) => inMark[i]);
        const seen = Uint8Array.from(inMark);
        for (let d = 1; d <= near && ring.length; d++) {
          const next: number[] = [];
          for (const i of ring) {
            const x0 = i % cols;
            for (const [dx, dy] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
              const xx = x0 + dx;
              const j = i + dx + dy * cols;
              if (xx < 0 || xx >= cols || j < 0 || j >= cols * rows || seen[j]) continue;
              seen[j] = 1;
              around[j] = 1 - (d - 1) / near;
              next.push(j);
            }
          }
          ring = next;
        }
      }
      // The mark leans about its own middle.
      pivot = [(cx * CELL), (cy * CELL)];
      canvas.style.transformOrigin = `${pivot[0]}px ${pivot[1]}px`;

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
          // Outside the mark only the rain itself, close to it and faint.
          if (!inMark[i]) {
            if (!rain || !around[i]) continue;
            const a = rain * around[i] * (behind < 1 ? 0.3 : 0.18);
            c.fillStyle = `rgba(${t.mark},${a.toFixed(3)})`;
            c.fillText(digits[i] ? "1" : "0", x * CELL + CELL / 2, y * CELL + CELL / 2);
            continue;
          }
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

    // The lean: toward the pointer, followed softly, at most MAX degrees.
    const MAX = 9;
    const aim = { x: 0, y: 0 };
    const lean = { x: 0, y: 0 };
    const onPointer = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect();
      aim.x = Math.max(-1, Math.min(1, (e.clientX - (r.left + pivot[0])) / (window.innerWidth / 2)));
      aim.y = Math.max(-1, Math.min(1, (e.clientY - (r.top + pivot[1])) / (window.innerHeight / 2)));
    };
    const onLeave = () => {
      aim.x = 0;
      aim.y = 0;
    };
    if (tilt && !still) {
      window.addEventListener("pointermove", onPointer, { passive: true });
      document.documentElement.addEventListener("pointerleave", onLeave);
    }

    let frame = 0;
    let last = performance.now();
    let acc = 0;
    const tick = (now: number) => {
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      if (tilt) {
        const k = 1 - Math.exp(-dt * 3);
        lean.x += (aim.x - lean.x) * k;
        lean.y += (aim.y - lean.y) * k;
        // Turned to face the pointer: right of it, its face turns right;
        // under it, down.
        canvas.style.transform = `perspective(1400px) rotateY(${(lean.x * MAX).toFixed(2)}deg) rotateX(${(-lean.y * MAX).toFixed(2)}deg)`;
      }
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
      window.removeEventListener("pointermove", onPointer);
      document.documentElement.removeEventListener("pointerleave", onLeave);
    };
  }, [anchor, scale, offset, shift, height, x, centred?.height, centred?.offset, near, tilt]);

  return (
    <canvas
      ref={ref}
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-x-0 top-0 -z-10 w-full", className)}
    />
  );
}
