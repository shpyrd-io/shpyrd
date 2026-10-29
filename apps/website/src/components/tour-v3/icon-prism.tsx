"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

// Each slide's label icon, large, given volume (Giovani, 2026-10-27): the
// very icon the label uses (lucide, its lines 2 wide in its 24 box, round
// ends and joins), extruded to 1.5 times its line's width, first drawn for
// slide 2's warning sign and then for every slide's.
//
// The walls are lit by a light at the window's upper left: each piece of
// wall is shaded by how much it turns to that light, so as the icon turns
// the light moves over them. The icon's line is followed along its edges,
// each edge cut into short pieces of wall with the way it faces; the pieces
// are drawn far to near, then the face over them. Drawn on a canvas, by
// hand, no 3D library; still for who asked for less motion.

// Each slide's icon (its label's, from lucide), its elements as paths in
// the icon's 24 box: circles and rounded squares written as paths too.
const circle = (cx: number, cy: number, r: number) =>
  `M${cx - r} ${cy}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`;
const square = (x: number, y: number, s: number, rx: number) =>
  `M${x + rx} ${y}h${s - 2 * rx}a${rx} ${rx} 0 0 1 ${rx} ${rx}v${s - 2 * rx}a${rx} ${rx} 0 0 1 ${-rx} ${rx}h${-(s - 2 * rx)}a${rx} ${rx} 0 0 1 ${-rx} ${-rx}v${-(s - 2 * rx)}a${rx} ${rx} 0 0 1 ${rx} ${-rx}z`;
export const ICONS = {
  "triangle-alert": [
    "m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3",
    "M12 9v4",
    "M12 17h.01",
  ],
  "scan-search": [
    "M3 7V5a2 2 0 0 1 2-2h2",
    "M17 3h2a2 2 0 0 1 2 2v2",
    "M21 17v2a2 2 0 0 1-2 2h-2",
    "M7 21H5a2 2 0 0 1-2-2v-2",
    circle(12, 12, 3),
    "m16 16-1.9-1.9",
  ],
  layers: [
    "M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z",
    "M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12",
    "M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17",
  ],
  fingerprint: [
    "M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4",
    "M14 13.12c0 2.38 0 6.38-1 8.88",
    "M17.29 21.02c.12-.6.43-2.3.5-3.02",
    "M2 12a10 10 0 0 1 18-6",
    "M2 16h.01",
    "M21.8 16c.2-2 .131-5.354 0-6",
    "M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2",
    "M8.65 22c.21-.66.45-1.32.57-2",
    "M9 6.8a6 6 0 0 1 9 5.2v2",
  ],
  chart: ["M5 21v-6", "M12 21V9", "M19 21V3"],
  rocket: [
    "M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5",
    "M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09",
    "M9 12a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.4 22.4 0 0 1-4 2z",
    "M9 12H4s.55-3.03 2-4c1.62-1.08 5 .05 5 .05",
  ],
  cloud: ["M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"],
  "layout-grid": [
    square(3, 3, 7, 1),
    square(14, 3, 7, 1),
    square(14, 14, 7, 1),
    square(3, 14, 7, 1),
  ],
  users: [
    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2",
    "M16 3.128a4 4 0 0 1 0 7.744",
    "M22 21v-2a4 4 0 0 0-3-3.87",
    circle(9, 7, 4),
  ],
};
export type IconName = keyof typeof ICONS;
const LINE = 2;
const HALF = LINE / 2;
const DEPTH = LINE * 1.5;
// The outline round every face: a hairline, as the containers' (CSS px).
const HAIRLINE = 0.7;

const hex = (c: string) =>
  [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
const mix = (a: string, b: string, t: number) => {
  const x = hex(a);
  const y = hex(b);
  return `rgb(${x.map((v, i) => Math.round(v + (y[i] - v) * t)).join(",")})`;
};

// An edge of the icon's line: points in the icon's units (x right, y down)
// with the way the wall there faces, out of the line.
type Edge = { x: number; y: number; nx: number; ny: number }[];

// The edges, worked out once for any icon: each of its paths followed along
// its centre line. A closed one (a ring, the triangle) is pushed out and in
// by half the line, two edges; an open one is a capsule round its centre
// line, round at both ends; a dot is a circle.
function edges(ds: string[]): Edge[] {
  const out: Edge[] = [];
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  for (const d of ds) {
    path.setAttribute("d", d);
    const length = path.getTotalLength();
    if (length < 0.1) {
      const c = path.getPointAtLength(0);
      out.push(
        Array.from({ length: 41 }, (_, i) => {
          const t = (Math.PI * 2 * i) / 40;
          return {
            x: c.x + Math.cos(t) * HALF,
            y: c.y + Math.sin(t) * HALF,
            nx: Math.cos(t),
            ny: Math.sin(t),
          };
        }),
      );
      continue;
    }
    const n = Math.max(24, Math.round(length * 10));
    const first = path.getPointAtLength(0);
    const last = path.getPointAtLength(length);
    const closed = Math.hypot(first.x - last.x, first.y - last.y) < 0.05;
    const pts = closed
      ? Array.from({ length: n }, (_, i) =>
          path.getPointAtLength((length * i) / n),
        )
      : Array.from({ length: n + 1 }, (_, i) =>
          path.getPointAtLength((length * i) / n),
        );
    const m = pts.length;
    // Its sideways direction at each point, left of the way it runs (y down).
    const side = pts.map((p, i) => {
      const a = closed ? pts[(i - 1 + m) % m] : pts[Math.max(0, i - 1)];
      const b = closed ? pts[(i + 1) % m] : pts[Math.min(m - 1, i + 1)];
      const tx = b.x - a.x;
      const ty = b.y - a.y;
      const l = Math.hypot(tx, ty) || 1;
      return { x: ty / l, y: -tx / l };
    });
    if (closed) {
      // The way round (its signed area, y down), so "out" is out of it.
      const area = pts.reduce((s, p, i) => {
        const q = pts[(i + 1) % m];
        return s + p.x * q.y - q.x * p.y;
      }, 0);
      const way = area > 0 ? 1 : -1;
      const outer: Edge = [];
      const inner: Edge = [];
      pts.forEach((p, i) => {
        const ox = side[i].x * way;
        const oy = side[i].y * way;
        outer.push({ x: p.x + ox * HALF, y: p.y + oy * HALF, nx: ox, ny: oy });
        inner.push({
          x: p.x - ox * HALF,
          y: p.y - oy * HALF,
          nx: -ox,
          ny: -oy,
        });
      });
      outer.push(outer[0]);
      inner.push(inner[0]);
      out.push(outer, inner);
      continue;
    }
    // Open: along one side, round the far end, back along the other side,
    // round the near end.
    const edge: Edge = [];
    const cap = (
      p: { x: number; y: number },
      from: { x: number; y: number },
    ) => {
      const a0 = Math.atan2(from.y, from.x);
      for (let k = 1; k < 12; k++) {
        const t = a0 + (Math.PI * k) / 12;
        edge.push({
          x: p.x + Math.cos(t) * HALF,
          y: p.y + Math.sin(t) * HALF,
          nx: Math.cos(t),
          ny: Math.sin(t),
        });
      }
    };
    pts.forEach((p, i) =>
      edge.push({
        x: p.x + side[i].x * HALF,
        y: p.y + side[i].y * HALF,
        nx: side[i].x,
        ny: side[i].y,
      }),
    );
    cap(pts[m - 1], side[m - 1]);
    for (let i = m - 1; i >= 0; i--) {
      const p = pts[i];
      edge.push({
        x: p.x - side[i].x * HALF,
        y: p.y - side[i].y * HALF,
        nx: -side[i].x,
        ny: -side[i].y,
      });
    }
    cap(pts[0], { x: -side[0].x, y: -side[0].y });
    edge.push(edge[0]);
    out.push(edge);
  }
  return out;
}

type R = [
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
];
const turn = (ay: number, ax: number): R => {
  const cy = Math.cos(ay),
    sy = Math.sin(ay),
    cx = Math.cos(ax),
    sx = Math.sin(ax);
  // Rx(ax) · Ry(ay)
  return [cy, 0, sy, sx * sy, cx, -sx * cy, -cx * sy, sx, cx * cy];
};

// The light: up and to the left of the window, and a little in front of
// it, low enough to graze the walls (Giovani, 2026-10-27), so those that
// turn to it and those that turn away differ more.
const L = (() => {
  const v = [-0.7, 0.62, 0.3];
  const l = Math.hypot(...v);
  return v.map((x) => x / l);
})();

type Piece = {
  pts: number[][];
  depth: number;
  colour: string;
  back: number[][];
};

function draw(
  ctx: CanvasRenderingContext2D,
  paths: Path2D[],
  walls: Edge[],
  w: number,
  h: number,
  dpr: number,
  ay: number,
  ax: number,
  pal: typeof KEY,
) {
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, w * dpr, h * dpr);
  const s = (Math.min(w, h) / 26) * dpr;
  const EDGE = (HAIRLINE * dpr) / s;
  const cx = (w / 2) * dpr;
  const cy = (h / 2) * dpr;
  const r = turn(ay, ax);
  // The near face's depth and the far one's.
  const toward = r[8] >= 0 ? 1 : -1;
  const near = (toward * DEPTH) / 2;
  const far = -near;

  // A point of the icon (x right, y down) at depth z: where it is on the
  // screen, and how near.
  const view = (X: number, Y: number, z: number) => {
    const x = X - 12;
    const y = -(Y - 12);
    return [
      cx + s * (r[0] * x + r[1] * y + r[2] * z),
      cy - s * (r[3] * x + r[4] * y + r[5] * z),
      r[6] * x + r[7] * y + r[8] * z,
    ];
  };
  // A direction in the icon's plane (x right, y down), turned.
  const dir = (nx: number, ny: number) => [
    r[0] * nx - r[1] * ny,
    r[3] * nx - r[4] * ny,
    r[6] * nx - r[7] * ny,
  ];

  // Every piece of wall that faces the eye, with its shade and its depth.
  const pieces: Piece[] = [];
  // Where a wall turns from facing the eye to facing away: the outline runs
  // there from the near face to the far one (Giovani, 2026-10-27), so the
  // two faces' outlines are joined all round, as a solid's are.
  const turns: number[][][] = [];
  for (const edge of walls) {
    const faces = edge.slice(0, -1).map((a, i) => {
      const b = edge[i + 1];
      return dir((a.nx + b.nx) / 2, (a.ny + b.ny) / 2)[2] > 0;
    });
    faces.forEach((f, i) => {
      const next = faces[(i + 1) % faces.length];
      if (f === next) return;
      // The point the two pieces share.
      const p = edge[i + 1];
      turns.push([view(p.x, p.y, near), view(p.x, p.y, far)]);
    });
    for (let i = 0; i < edge.length - 1; i++) {
      const a = edge[i];
      const b = edge[i + 1];
      const n = dir((a.nx + b.nx) / 2, (a.ny + b.ny) / 2);
      if (n[2] <= 0) continue;
      const lit = Math.max(0, n[0] * L[0] + n[1] * L[1] + n[2] * L[2]);
      const a1 = view(a.x, a.y, near);
      const b1 = view(b.x, b.y, near);
      const b0 = view(b.x, b.y, far);
      const a0 = view(a.x, a.y, far);
      pieces.push({
        pts: [a1, b1, b0, a0],
        depth: (a1[2] + b1[2] + a0[2] + b0[2]) / 4,
        colour: mix(pal.shade, pal.lit, Math.min(1, lit * 1.15)),
        back: [a0, b0],
      });
    }
  }
  pieces.sort((p, q) => p.depth - q.depth);
  ctx.lineJoin = "round";
  for (const p of pieces) {
    ctx.beginPath();
    p.pts.forEach(([x, y], k) => (k ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
    ctx.closePath();
    ctx.fillStyle = p.colour;
    ctx.fill();
    // The same colour round it, so no seam shows between two pieces.
    ctx.lineWidth = dpr * 0.75;
    ctx.strokeStyle = p.colour;
    ctx.stroke();
    // The wall's far edge, a hairline, as the containers' edges.
    ctx.beginPath();
    ctx.moveTo(p.back[0][0], p.back[0][1]);
    ctx.lineTo(p.back[1][0], p.back[1][1]);
    ctx.lineWidth = HAIRLINE * dpr;
    ctx.strokeStyle = pal.outline;
    ctx.stroke();
  }

  // The outline down the walls where they turn away.
  ctx.lineWidth = HAIRLINE * dpr;
  ctx.strokeStyle = pal.outline;
  ctx.lineCap = "round";
  for (const [a, b] of turns) {
    ctx.beginPath();
    ctx.moveTo(a[0], a[1]);
    ctx.lineTo(b[0], b[1]);
    ctx.stroke();
  }

  // The near face over the walls: the icon itself, white, its hairline.
  ctx.setTransform(
    s * r[0],
    -s * r[3],
    -s * r[1],
    s * r[4],
    cx + s * (-12 * r[0] + 12 * r[1] + r[2] * near),
    cy - s * (-12 * r[3] + 12 * r[4] + r[5] * near),
  );
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  ctx.lineWidth = LINE + EDGE * 2;
  ctx.strokeStyle = pal.outline;
  for (const p of paths) ctx.stroke(p);
  ctx.lineWidth = LINE;
  ctx.strokeStyle = pal.face;
  for (const p of paths) ctx.stroke(p);
}

// In characters (Giovani, 2026-10-27, after modal.com's hero): the solid
// is drawn out of sight in key colours (the face orange, the walls grey by
// their light), then read cell by cell and written again as characters, so
// it shows as a field of characters whose weight and grey give its depth.
// Only greys, and only 0s and 1s, as binary (Giovani, 2026-10-27): the light
// and the shadow are in the font's weight, Geist Mono's whole range, 100 on
// the lit walls to 900 on the face; the digits flip now and then, as on the
// reference.
const KEY = {
  face: "#ff4f00",
  lit: "#e0e0e0",
  shade: "#505050",
  outline: "#202020",
};
// Fine (Giovani, 2026-10-27): 6×9 px cells in an 8 px mono, so the more
// detailed icons' close lines and curves keep apart.
const CELL_W = 6;
const CELL_H = 9;
const FONT = 8;
const INK = {
  // Soft (Giovani, 2026-10-27): light greys, a background's.
  // Subtle (Giovani, 2026-10-27): every grey halfway to the page's ground,
  // so it stays a background; the weights keep the volume.
  light: { lit: "#f3f4f6", shade: "#d8dadc", face: ["#b4b5b9", "#bbbcc0"] },
  // Dark: the binary ocean's orange (binary-ocean.tsx, #ff7a3d) at the
  // same strengths as the greys were (Giovani, 2026-10-27).
  dark: { lit: "#221510", shade: "#472618", face: ["#844224", "#713920"] },
};
const hash = (i: number) => {
  const x = Math.sin(i * 127.1) * 43758.5453;
  return x - Math.floor(x);
};

function write(
  out: CanvasRenderingContext2D,
  key: CanvasRenderingContext2D,
  w: number,
  h: number,
  dpr: number,
  t: number,
  dark: boolean,
) {
  const ink = dark ? INK.dark : INK.light;
  const cols = Math.floor(w / CELL_W);
  const rows = Math.floor(h / CELL_H);
  const px = key.getImageData(0, 0, w, h).data;
  out.setTransform(1, 0, 0, 1, 0, 0);
  out.clearRect(0, 0, w * dpr, h * dpr);
  out.textAlign = "center";
  out.textBaseline = "middle";
  const tick = Math.floor(t * 5);
  // The cells to write, by weight, so the font is set once a weight.
  const byWeight: { x: number; y: number; glyph: string; colour: string }[][] =
    Array.from({ length: 9 }, () => []);
  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < cols; col++) {
      // The cell's average, by how much of it is covered.
      let r = 0,
        g = 0,
        b = 0,
        a = 0;
      for (let y = row * CELL_H; y < (row + 1) * CELL_H; y += 2) {
        for (let x = col * CELL_W; x < (col + 1) * CELL_W; x += 2) {
          const k = (y * w + x) * 4;
          const al = px[k + 3];
          r += px[k] * al;
          g += px[k + 1] * al;
          b += px[k + 2] * al;
          a += al;
        }
      }
      const cover = a / (255 * Math.ceil(CELL_W / 2) * Math.ceil(CELL_H / 2));
      if (cover < 0.12) continue;
      r /= a;
      g /= a;
      b /= a;
      const id = row * cols + col;
      const seed = hash(id);
      // Now and then a cell flips between 0 and 1.
      const step = Math.floor(tick / (2 + Math.floor(seed * 6)) + seed * 10);
      const glyph = hash(id + step * 31) > 0.5 ? "1" : "0";
      let dark: number;
      let colour: string;
      if ((r - b) / 255 > 0.45) {
        // The face: always the heaviest and darkest, 800 and 900, so it
        // stands apart from any wall, even one in shadow (Giovani,
        // 2026-10-27).
        dark = 0.92 + hash(id + 7) * 0.08;
        colour = mix(ink.face[0], ink.face[1], hash(id + 7) * 0.8);
      } else {
        // 0 in the light, 1 in the dark.
        const flat = Math.min(
          1,
          Math.max(0, (224 - (r + g + b) / 3) / (224 - 80)),
        );
        // More contrast in the solid's shadows (Giovani, 2026-10-27): the
        // lit walls pushed lighter, those in shadow darker.
        const shade = Math.min(1, Math.max(0, (flat - 0.45) * 1.8 + 0.45));
        // The walls from 100 in the light to 600 in shadow, below the face.
        dark = Math.min(
          0.62,
          shade * 0.62 * Math.min(1, cover * 1.3) + (hash(id + 3) - 0.5) * 0.06,
        );
        colour = mix(ink.lit, ink.shade, shade);
      }
      // The light and the shadow in the weight (Giovani, 2026-10-27): 100
      // where lightest, 900 where darkest, every weight between.
      const weight = Math.max(0, Math.min(8, Math.round(dark * 8)));
      byWeight[weight].push({
        x: (col + 0.5) * CELL_W * dpr,
        y: (row + 0.5) * CELL_H * dpr,
        glyph,
        colour,
      });
    }
  }
  byWeight.forEach((cells, i) => {
    if (!cells.length) return;
    out.font = `${(i + 1) * 100} ${FONT * dpr}px "Geist Mono Variable", ui-monospace, monospace`;
    for (const c of cells) {
      out.fillStyle = c.colour;
      out.fillText(c.glyph, c.x, c.y);
    }
  });
}

// `active`: on the slide in view; off, it keeps its last frame and stops.
export function IconPrism({
  icon,
  active = true,
  className,
}: {
  icon: IconName;
  active?: boolean;
  className?: string;
}) {
  const canvas = React.useRef<HTMLCanvasElement>(null);

  React.useEffect(() => {
    const el = canvas.current;
    const ctx = el?.getContext("2d");
    // The solid in key colours, out of sight, at one pixel a CSS pixel.
    const off = document.createElement("canvas");
    const key = off.getContext("2d", { willReadFrequently: true });
    if (!el || !ctx || !key) return;
    // The mono's weights, before the first frame that needs them.
    void document.fonts.load(`100 ${FONT}px "Geist Mono Variable"`);
    const paths = ICONS[icon].map((d) => new Path2D(d));
    const walls = edges(ICONS[icon]);
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const dark = () => document.documentElement.classList.contains("dark");
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    let w = 0;
    let h = 0;
    const size = () => {
      const b = el.getBoundingClientRect();
      w = Math.round(b.width);
      h = Math.round(b.height);
      el.width = Math.round(w * dpr);
      el.height = Math.round(h * dpr);
      off.width = w;
      off.height = h;
    };
    // It follows the pointer (Giovani, 2026-10-27, as Modal's): turned
    // toward wherever the pointer is on the page, eased, over a slow sway of
    // its own.
    const aim = { x: 0, y: 0 };
    const eye = { x: 0, y: 0 };
    const follow = (e: PointerEvent) => {
      const b = el.getBoundingClientRect();
      aim.x = Math.max(
        -1,
        Math.min(
          1,
          (e.clientX - (b.left + b.width / 2)) / (window.innerWidth / 2),
        ),
      );
      aim.y = Math.max(
        -1,
        Math.min(
          1,
          (e.clientY - (b.top + b.height / 2)) / (window.innerHeight / 2),
        ),
      );
    };
    if (!still)
      window.addEventListener("pointermove", follow, { passive: true });
    const paint = (t: number) => {
      if (!w || !h) return;
      eye.x += (aim.x - eye.x) * 0.06;
      eye.y += (aim.y - eye.y) * 0.06;
      const ay = Math.sin(t * 0.22) * 0.18 + eye.x * 0.75;
      const ax = -0.12 + Math.sin(t * 0.17) * 0.06 + eye.y * 0.5;
      draw(key, paths, walls, w, h, 1, ay, ax, KEY);
      write(ctx, key, w, h, dpr, t, dark());
    };
    size();
    const seen = new ResizeObserver(() => {
      size();
      paint(2);
    });
    seen.observe(el);
    let frame = 0;
    const start = performance.now();
    const tick = (now: number) => {
      paint(2 + (now - start) / 1000);
      frame = requestAnimationFrame(tick);
    };
    if (still || !active) paint(2);
    else frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      seen.disconnect();
      window.removeEventListener("pointermove", follow);
    };
  }, [icon, active]);

  return (
    <canvas
      ref={canvas}
      aria-hidden="true"
      className={cn("pointer-events-none", className)}
    />
  );
}
