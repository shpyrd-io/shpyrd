"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

// A background of the yard's container (design/ui's shipyard sprite
// container.svg) made solid: a chain of them, in 3D, turning slowly like the
// treads of a spiral staircase, and posed again as the reader scrolls the page.
// After aniwall.aura.build's helix of cards; drawn the yard's way, flat and
// outlined, not lit like glass.
//
// The container is a real 20ft ISO box: 6.058 m long, 2.591 m high, 2.438 m
// wide, with corner posts and castings, top and bottom rails, a corrugated
// steel side wall carrying the mark, a corrugated front wall, a roof pressed
// across, and at the back the two doors with their four locking bars, handles,
// cam keepers and hinges. Each container is seen without perspective of its
// own (the camera is far), larger or smaller by how near it is, so every face
// is a flat drawing laid on it: lines stay lines, the mark stays the mark.
//
// Drawn on a canvas, by hand, in a few hundred lines; no 3D library. Still, at
// its first pose, for who asked the system for less motion.

const L = 6.058;
const H = 2.591;
const W = 2.438;

type V3 = [number, number, number];
type M3 = [number, number, number, number, number, number, number, number, number];

const mul = (a: M3, b: M3): M3 => [
  a[0] * b[0] + a[1] * b[3] + a[2] * b[6], a[0] * b[1] + a[1] * b[4] + a[2] * b[7], a[0] * b[2] + a[1] * b[5] + a[2] * b[8],
  a[3] * b[0] + a[4] * b[3] + a[5] * b[6], a[3] * b[1] + a[4] * b[4] + a[5] * b[7], a[3] * b[2] + a[4] * b[5] + a[5] * b[8],
  a[6] * b[0] + a[7] * b[3] + a[8] * b[6], a[6] * b[1] + a[7] * b[4] + a[8] * b[7], a[6] * b[2] + a[7] * b[5] + a[8] * b[8],
];
const apply = (m: M3, v: V3): V3 => [
  m[0] * v[0] + m[1] * v[1] + m[2] * v[2],
  m[3] * v[0] + m[4] * v[1] + m[5] * v[2],
  m[6] * v[0] + m[7] * v[1] + m[8] * v[2],
];
const rotX = (a: number): M3 => [1, 0, 0, 0, Math.cos(a), -Math.sin(a), 0, Math.sin(a), Math.cos(a)];
const rotY = (a: number): M3 => [Math.cos(a), 0, Math.sin(a), 0, 1, 0, -Math.sin(a), 0, Math.cos(a)];
const rotZ = (a: number): M3 => [Math.cos(a), -Math.sin(a), 0, Math.sin(a), Math.cos(a), 0, 0, 0, 1];

// The yard's mark (design/ui brand.tsx), in its own 140.71 × 140.64 box.
const MARK = [
  "M127.94,102.31c0,7.06-5.72,12.77-12.77,12.77h-25.65c-7.63,0-14.48,3.35-19.16,8.66-4.68-5.31-11.53-8.66-19.16-8.66h-25.64c-7.05,0-12.77-5.72-12.77-12.77v-16H0v16c0,14.11,11.44,25.55,25.55,25.55h25.64c7.05,0,12.77,5.72,12.77,12.77h12.77c0-7.060,5.72-12.77,12.77-12.77h25.64c14.11,0,25.55-11.44,25.55-25.55v-16h-12.770v16Z",
  "M132.87,38.37h0s-17.71,0-17.71,0v-12.77c7.05,0,12.77-5.72,12.77-12.77h0s-51.19,0-51.19,0V0h-12.77v12.82H12.77c0,7.06,5.72,12.77,12.77,12.77v12.77H7.84s-7.84,0-7.84,0v41.55h12.77v-28.78h115.16v28.78h12.77v-41.55h-7.84ZM38.32,25.6h64.06v12.770H38.32v-12.77Z",
  "M54.35,102.33L68.44,102.33L86.36,63.91L72.26,63.91Z",
  "M31.27,83.17L40.21,102.33L54.3,102.33L45.37,83.17L54.35,63.91L40.26,63.91Z",
  "M100.45,102.33L109.43,83.07L100.5,63.91L86.4,63.91L95.34,83.07L86.36,102.33Z",
].join("");

type Palette = {
  side: string;
  end: string;
  outline: string;
  onSide: string;
  onEnd: string;
  mark: string;
  shade: string;
  shadeBy: number;
  page: string;
};
// The sprite's own colours, light and dark (container.svg, container.dark.svg).
const LIGHT: Palette = {
  side: "#d3d8de", end: "#ffffff", outline: "#3f4044", onSide: "#ffffff", onEnd: "#d3d8de",
  mark: "#ff4f00", shade: "#3f4044", shadeBy: 0.14, page: "#ffffff",
};
const DARK: Palette = {
  side: "#2c2c2e", end: "#000000", outline: "#636366", onSide: "#000000", onEnd: "#2c2c2e",
  mark: "#ff4f00", shade: "#ffffff", shadeBy: 0.06, page: "#000000",
};

const hex = (c: string) => [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
const mix = (a: string, b: string, t: number) => {
  const [x, y] = [hex(a), hex(b)];
  return `#${x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, "0")).join("")}`;
};

// A face: where its corner is, its two edges (seen from outside: right, up),
// how it is drawn in its own metres, and its outward normal.
type Face = {
  o: V3;
  u: V3;
  v: V3;
  n: V3;
  kind: "side" | "door" | "front" | "roof" | "floor";
};
const FACES: Face[] = [
  { kind: "side", o: [-L / 2, -H / 2, W / 2], u: [L, 0, 0], v: [0, H, 0], n: [0, 0, 1] },
  { kind: "side", o: [L / 2, -H / 2, -W / 2], u: [-L, 0, 0], v: [0, H, 0], n: [0, 0, -1] },
  { kind: "door", o: [-L / 2, -H / 2, -W / 2], u: [0, 0, W], v: [0, H, 0], n: [-1, 0, 0] },
  { kind: "front", o: [L / 2, -H / 2, W / 2], u: [0, 0, -W], v: [0, H, 0], n: [1, 0, 0] },
  { kind: "roof", o: [-L / 2, H / 2, W / 2], u: [L, 0, 0], v: [0, 0, -W], n: [0, 1, 0] },
  { kind: "floor", o: [-L / 2, -H / 2, -W / 2], u: [L, 0, 0], v: [0, 0, W], n: [0, -1, 0] },
];

// What stands out of each face, in its metres (u right, v up from its bottom
// left): a box from (u0, v0) to (u1, v1), raised d, its top narrower by iu on
// its u sides (the trapezoid of a corrugation). As the HO model of a 20ft
// container (thingiverse 5197932) has them: the ribs of the walls and the
// roof, the corner posts and rails, the door's bars and hinges. The corner
// castings are blocks of their own (castings(), below).
type Raised = [u0: number, v0: number, u1: number, v1: number, d: number, iu?: number];

function relief(kind: Face["kind"], a: number, b: number): Raised[] {
  const post = 0.13;
  const out: Raised[] = [];
  if (kind === "side") {
    const pitch = 0.278;
    const n = Math.floor((a - 2 * (post + 0.04)) / pitch);
    const start = (a - n * pitch) / 2;
    for (let i = 0; i < n; i++) out.push([start + i * pitch + 0.035, 0.17, start + i * pitch + 0.245, b - 0.13, 0.09, 0.045]);
    out.push([post, b - 0.13, a - post, b, 0.06], [post, 0, a - post, 0.17, 0.06]);
    out.push([0, 0, post, b, 0.08], [a - post, 0, a, b, 0.08]);
  } else if (kind === "front") {
    const n = 5;
    const room = (a - 2 * post) / n;
    for (let i = 0; i < n; i++) out.push([post + i * room + 0.06, 0.17, post + (i + 1) * room - 0.06, b - 0.13, 0.08, 0.05]);
    out.push([post, b - 0.13, a - post, b, 0.06], [post, 0, a - post, 0.17, 0.06]);
    out.push([0, 0, post, b, 0.08], [a - post, 0, a, b, 0.08]);
  } else if (kind === "door") {
    out.push([post, b - 0.22, a - post, b, 0.06], [post, 0, a - post, 0.16, 0.06]);
    out.push([0, 0, post, b, 0.08], [a - post, 0, a, b, 0.08]);
    // Two locking bars a door, with their cam keepers and handles.
    for (const u of [post + 0.3, a / 2 - 0.28, a / 2 + 0.28, a - post - 0.3]) {
      out.push([u - 0.025, 0.24, u + 0.025, b - 0.32, 0.07]);
      out.push([u - 0.055, 0.18, u + 0.055, 0.3, 0.1], [u - 0.055, b - 0.36, u + 0.055, b - 0.24, 0.1]);
      const side = u < a / 2 ? 1 : -1;
      out.push([Math.min(u, u + side * 0.22), 1.12, Math.max(u, u + side * 0.22), 1.17, 0.11]);
    }
    // Four hinges a door.
    for (const v of [0.45, 0.95, 1.55, 2.05]) out.push([post, v, post + 0.1, v + 0.12, 0.09], [a - post - 0.1, v, a - post, v + 0.12, 0.09]);
  } else if (kind === "roof") {
    for (let u = 0.3; u < a - 0.4; u += 0.4) out.push([u, 0.12, u + 0.24, b - 0.12, 0.05, 0.05]);
    out.push([0, 0, a, 0.12, 0.04], [0, b - 0.12, a, b, 0.04]);
  }
  return out;
}

// The eight corner castings: blocks at the box's corners, standing a little
// proud of every face that meets there, as on the model.
const CASTINGS: [V3, V3][] = [];
for (const x of [-1, 1])
  for (const y of [-1, 1])
    for (const z of [-1, 1]) {
      const out: V3 = [x * (L / 2 + 0.1), y * (H / 2 + 0.1), z * (W / 2 + 0.1)];
      const inn: V3 = [x * (L / 2 - 0.24), y * (H / 2 - 0.17), z * (W / 2 - 0.22)];
      CASTINGS.push([
        [Math.min(out[0], inn[0]), Math.min(out[1], inn[1]), Math.min(out[2], inn[2])],
        [Math.max(out[0], inn[0]), Math.max(out[1], inn[1]), Math.max(out[2], inn[2])],
      ]);
    }

// Where the chain is, as the page is read: a pose for its top, and for each
// quarter of the way down, between which it eases.
type Pose = { px: number; py: number; pz: number; rx: number; ry: number; rz: number; ph: number };
const POSES: Pose[] = [
  { px: 4, py: 2, pz: 0, rx: 0.3, ry: 0.24, rz: -0.55, ph: 0 },
  { px: 6, py: -10, pz: 2, rx: -0.25, ry: 0.18, rz: -0.32, ph: 1.6 },
  { px: -10, py: 4, pz: 2, rx: 0.6, ry: 0.12, rz: -1.2, ph: 3 },
  { px: 3, py: -8, pz: 1, rx: 1.15, ry: 0.16, rz: -0.3, ph: 4.2 },
  { px: 0, py: 9, pz: 1, rx: 0.28, ry: 0.22, rz: -0.6, ph: 5.4 },
];
const smooth = (t: number) => t * t * (3 - 2 * t);
function poseAt(progress: number): Pose {
  const f = Math.min(Math.max(progress, 0), 1) * (POSES.length - 1);
  const i = Math.min(Math.floor(f), POSES.length - 2);
  const t = smooth(f - i);
  const [a, b] = [POSES[i], POSES[i + 1]];
  const out = {} as Pose;
  for (const k of Object.keys(a) as (keyof Pose)[]) out[k] = a[k] + (b[k] - a[k]) * t;
  return out;
}

const COUNT = 25;
const SPACING = H + 1.12;
// Each container stands with its length across the chain, its height along
// it, its marked wall toward the camera at rest: x → y, y → -x, z → z.
const BASE: M3 = rotZ(Math.PI / 2);
const STEP = 0.24;
const CAMERA = 70;
// Where the ring's eye level stands on the screen, from its top.
const RISE = 0.04;
// How many metres of the chain the screen's height holds, at the chain.
const VIEW = 32;
const LIGHT_DIR: V3 = (() => {
  const v: V3 = [-0.35, 0.6, 0.72];
  const l = Math.hypot(...v);
  return [v[0] / l, v[1] / l, v[2] / l];
})();

export function ContainerHelix({
  className,
  ring = false,
}: {
  className?: string;
  // The container circle (ContainerCircle, below): the containers in a ring
  // around the reader instead of the spiral.
  ring?: boolean;
}) {
  const ref = React.useRef<HTMLCanvasElement>(null);

  React.useEffect(() => {
    const canvas = ref.current;
    const c = canvas?.getContext("2d");
    if (!canvas || !c) return;
    const mark = new Path2D(MARK);
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let dark = document.documentElement.classList.contains("dark");
    const themed = new MutationObserver(() => {
      dark = document.documentElement.classList.contains("dark");
      if (still) draw();
    });
    themed.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });

    let dpr = 1;
    let width = 0;
    let height = 0;
    const resize = () => {
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      width = window.innerWidth;
      height = window.innerHeight;
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      if (still) draw();
    };

    const progress = () => {
      const room = document.documentElement.scrollHeight - window.innerHeight;
      return room > 0 ? window.scrollY / room : 0;
    };
    let pose = poseAt(progress());
    let drift = 0;
    // The ring turns slowly on its own, a container's step (a 40th of the
    // turn) every three seconds or so, and the scroll speeds it on: a step
    // for every 140px scrolled, followed softly, the way it was scrolled.
    // Left alone it goes on slowly the way it was last scrolled.
    const spinAt = () => (window.scrollY / 140) * ((Math.PI * 2) / 40);
    let spin = spinAt();
    let idle = 0;
    let way = 1;
    let lastY = window.scrollY;

    function draw() {
      if (!c) return;
      const pal = dark ? DARK : LIGHT;
      c.setTransform(1, 0, 0, 1, 0, 0);
      c.clearRect(0, 0, canvas!.width, canvas!.height);
      // Inside the ring the eye needs a wide view, so the ring reads as a
      // circle: its far side small in the middle, its sides sweeping by.
      const f = (height * CAMERA) / (ring ? 72 : VIEW);
      const group = mul(mul(rotZ(pose.rz), rotY(pose.ry)), rotX(pose.rx));
      const phase = pose.ph * 0.42 + drift;

      // Every container: where it stands, how it is turned, how near it is.
      const boxes: { m: M3; centre: V3 }[] = [];
      if (ring) {
        // A ring around the eye, a little under it, so it reads as a circle
        // seen from within: the near part passes at the sides, the far part
        // across the middle. Each container stands across the ring, its length
        // pointing to the centre, like the spokes of a fan, a gap between each.
        const N = 40;
        const R = 30;
        // The eye stands inside the ring, near its edge and over it, looking
        // ahead, level: the far side small across the middle, the
        // near side sweeping by large at the edges and going on behind.
        const turn = spin;
        // Each container, on its own, lifts by half as much again as its
        // height as it passes straight ahead, behind the hero's title, and
        // settles again: the lift starts and ends within about six
        // containers' room (three slots either side of the middle), level all
        // the while.
        const REACH = ((Math.PI * 2) / N) * 3;
        const lift = (t: number) => (Math.abs(t) < REACH ? (0.5 + 0.5 * Math.cos((Math.PI * t) / REACH)) * 1.5 * H : 0);
        for (let i = 0; i < N; i++) {
          const a = (i / N) * Math.PI * 2 + turn;
          const rel = [R * Math.sin(a), -8 + lift(Math.atan2(Math.sin(a), Math.cos(a))), -20 - R * Math.cos(a)];
          if (-rel[2] < 7) continue;
          boxes.push({ m: rotY(Math.PI / 2 - a), centre: [rel[0], rel[1], CAMERA + rel[2]] });
        }
      } else
      for (let i = 0; i < COUNT; i++) {
        const t = i - COUNT / 2;
        const angle = -0.9 + i * STEP + phase + 0.05 * Math.sin(i * 0.21 - phase * 2);
        const turn = rotX(angle);
        // Hung from the chain by one end, so the far ends trace the spiral.
        const hang = apply(turn, [0, L * 0.5, 0]);
        const at: V3 = [t * SPACING, 0.9 * Math.sin(t * 0.085) + hang[1], 0.5 * Math.sin(t * 0.06 + 1.2) + hang[2]];
        const world = apply(group, at);
        const centre: V3 = [world[0] + pose.px, world[1] + pose.py, world[2] + pose.pz];
        boxes.push({ m: mul(group, mul(turn, BASE)), centre });
      }
      // Farthest first; in the ring by distance from the eye, so neighbours
      // at the sides are laid in the right order.
      const away = (b: { centre: V3 }) =>
        ring ? Math.hypot(b.centre[0], b.centre[1], CAMERA - b.centre[2]) : CAMERA - b.centre[2];
      boxes.sort((a, b) => away(b) - away(a));

      for (const box of boxes) {
        const scale = (f / (CAMERA - box.centre[2])) * dpr;
        const sx = canvas!.width / 2 + box.centre[0] * scale;
        const sy = canvas!.height / 2 - box.centre[1] * scale;
        // In the ring every point is seen from the eye, true perspective, so
        // the ring turns as one body; the spiral, far off, keeps each box's
        // own scale.
        const to = (p: V3) => {
          const q = apply(box.m, p);
          if (!ring) return [sx + q[0] * scale, sy - q[1] * scale];
          const w: V3 = [box.centre[0] + q[0], box.centre[1] + q[1], box.centre[2] + q[2]];
          const k = (f / (CAMERA - w[2])) * dpr;
          // The eye looks level, its view shifted up (as a shift lens does), so
          // the containers' upright edges stay upright to the screen's edges.
          return [canvas!.width / 2 + w[0] * k, canvas!.height * RISE - w[1] * k];
        };
        // How much a direction turns to the eye: toward the camera itself in
        // the ring, along the view in the spiral.
        const toEye = (() => {
          const e: V3 = [-box.centre[0], -box.centre[1], CAMERA - box.centre[2]];
          const l = Math.hypot(...e);
          return ring ? [e[0] / l, e[1] / l, e[2] / l] : [0, 0, 1];
        })();
        const facing = (v: number[]) => v[0] * toEye[0] + v[1] * toEye[1] + v[2] * toEye[2];
        // Far ones fade into the page, as in a haze: their colours go to the
        // page's, so none shows through another.
        const near = ring
          ? Math.min(Math.max(1 - (CAMERA - box.centre[2] - 8) / 90, 0), 1)
          : Math.min(Math.max((box.centre[2] + 30) / 50, 0), 1);
        const haze = (colour: string) => mix(colour, pal.page, (1 - near) * 0.82);
        const stroke = Math.max(0.5 * dpr, scale * 0.02);
        const edge = haze(dark ? mix(pal.outline, "#000000", 0.2) : mix(pal.onEnd, pal.outline, 0.55));

        // A corner casting: drawn over the walls, only its three outer faces
        // (those that turn away from the box, as the walls beside them do),
        // and only those that face the camera, so it never pops in or out as
        // the box turns. The colour of the wall it continues.
        const castings = scale > 2.2 * dpr ? CASTINGS : [];
        const block = ([lo, hi]: [V3, V3]) => {
          c.setTransform(1, 0, 0, 1, 0, 0);
          const corner = (i: number): V3 => [i & 1 ? hi[0] : lo[0], i & 2 ? hi[1] : lo[1], i & 4 ? hi[2] : lo[2]];
          const sx = Math.sign(hi[0] + lo[0]), sy = Math.sign(hi[1] + lo[1]), sz = Math.sign(hi[2] + lo[2]);
          const outer: [V3, number[], string][] = [
            [[sx, 0, 0], sx > 0 ? [1, 3, 7, 5] : [0, 2, 6, 4], pal.end],
            [[0, sy, 0], sy > 0 ? [2, 3, 7, 6] : [0, 1, 5, 4], pal.end],
            [[0, 0, sz], sz > 0 ? [4, 5, 7, 6] : [0, 1, 3, 2], pal.side],
          ];
          for (const [nl, idx, base] of outer) {
            const nw = apply(box.m, nl);
            if (facing(nw) <= 0) continue;
            c.globalAlpha = Math.min(1, facing(nw) * 5);
            const lit = Math.max(0, nw[0] * LIGHT_DIR[0] + nw[1] * LIGHT_DIR[1] + nw[2] * LIGHT_DIR[2]);
            c.beginPath();
            idx.forEach((k, j) => {
              const [x, y] = to(corner(k));
              if (j) c.lineTo(x, y);
              else c.moveTo(x, y);
            });
            c.closePath();
            c.fillStyle = haze(dark ? mix(base, pal.shade, lit * pal.shadeBy) : mix(base, pal.shade, (1 - lit) * pal.shadeBy));
            c.fill();
            c.strokeStyle = edge;
            c.lineWidth = stroke * 0.5;
            c.stroke();
            c.globalAlpha = 1;
          }
        };

        for (const face of FACES) {
          const n = apply(box.m, face.n);
          // Seen from the eye at the face's own middle, so a near box's walls
          // show and hide as they should.
          const mid = apply(box.m, [face.o[0] + (face.u[0] + face.v[0]) / 2, face.o[1] + (face.u[1] + face.v[1]) / 2, face.o[2] + (face.u[2] + face.v[2]) / 2]);
          const view = ring ? [-box.centre[0] - mid[0], -box.centre[1] - mid[1], CAMERA - box.centre[2] - mid[2]] : [0, 0, 1];
          const seen = (n[0] * view[0] + n[1] * view[1] + n[2] * view[2]) / Math.hypot(view[0], view[1], view[2]);
          if (seen <= 0) continue;
          const a = Math.hypot(...face.u);
          const b = Math.hypot(...face.v);
          const [ox, oy] = to(face.o);
          const [ux, uy] = to([face.o[0] + face.u[0], face.o[1] + face.u[1], face.o[2] + face.u[2]]);
          const [vx, vy] = to([face.o[0] + face.v[0], face.o[1] + face.v[1], face.o[2] + face.v[2]]);
          const lit = Math.max(0, n[0] * LIGHT_DIR[0] + n[1] * LIGHT_DIR[1] + n[2] * LIGHT_DIR[2]);
          const base = face.kind === "side" ? pal.side : pal.end;
          const shaded = dark ? mix(base, pal.shade, lit * pal.shadeBy) : mix(base, pal.shade, (1 - lit) * pal.shadeBy);

          // A point of the face at (u, v), raised h along its normal. In the
          // spiral the face is mapped flat (each box seen from afar); in the
          // ring every point is seen from the eye, so a near face keeps its
          // true corners and its walls stay square.
          const U = [(ux - ox) / a, (uy - oy) / a];
          const V = [(vx - ox) / b, (vy - oy) / b];
          const N = [n[0] * scale, -n[1] * scale];
          const at = (u: number, v: number, h: number): [number, number] => {
            if (!ring) return [ox + U[0] * u + V[0] * v + N[0] * h, oy + U[1] * u + V[1] * v + N[1] * h];
            const q = to([
              face.o[0] + (face.u[0] * u) / a + (face.v[0] * v) / b + face.n[0] * h,
              face.o[1] + (face.u[1] * u) / a + (face.v[1] * v) / b + face.n[1] * h,
              face.o[2] + (face.u[2] * u) / a + (face.v[2] * v) / b + face.n[2] * h,
            ]);
            return [q[0], q[1]];
          };
          const outline = () => {
            c.setTransform(1, 0, 0, 1, 0, 0);
            c.beginPath();
            for (const [u, v] of [[0, 0], [a, 0], [a, b], [0, b]]) {
              const [x, y] = at(u, v, 0);
              if (u || v) c.lineTo(x, y);
              else c.moveTo(x, y);
            }
            c.closePath();
          };

          // The face laid on the screen.
          outline();
          c.fillStyle = haze(shaded);
          c.fill();

          // A wall seen nearly edge on packs its ribs into a moiré that
          // shimmers as it turns: its relief fades out before that.
          const flat = Math.min(1, Math.max(0, (seen - 0.12) / 0.3));
          if (face.kind !== "floor" && scale > 2.2 * dpr && flat > 0) {
            c.setTransform(1, 0, 0, 1, 0, 0);
            const uh = apply(box.m, face.u).map((x) => x / a);
            const vh = apply(box.m, face.v).map((x) => x / b);
            // The four flanks share their slope across a face: each is one
            // path, in one colour, drawn if it turns toward the camera.
            const sides = [
              { dir: uh.map((x) => -x), edge: 0 },
              { dir: uh, edge: 1 },
              { dir: vh.map((x) => -x), edge: 2 },
              { dir: vh, edge: 3 },
            ].map(({ dir, edge }) => {
              const tilt = edge < 2 ? 0.45 : 0;
              const nf = [dir[0] + n[0] * tilt, dir[1] + n[1] * tilt, dir[2] + n[2] * tilt];
              const len = Math.hypot(nf[0], nf[1], nf[2]);
              // Fading in as it turns to the camera, out as it turns away:
              // never popping.
              const fade = Math.min(1, Math.max(0, (facing(nf) / len) * 5));
              const shown = fade > 0;
              const lf = Math.max(0, (nf[0] * LIGHT_DIR[0] + nf[1] * LIGHT_DIR[1] + nf[2] * LIGHT_DIR[2]) / len);
              return { edge, shown, fade, lf, path: new Path2D() };
            });
            const tops = new Path2D();
            const quad = (p: Path2D, q: [number, number][]) => {
              p.moveTo(q[0][0], q[0][1]);
              for (let k = 1; k < 4; k++) p.lineTo(q[k][0], q[k][1]);
              p.closePath();
            };
            for (const [u0, v0, u1, v1, d, iu = 0] of relief(face.kind, a, b)) {
              const b0 = at(u0, v0, 0), b1 = at(u1, v0, 0), b2 = at(u1, v1, 0), b3 = at(u0, v1, 0);
              const t0 = at(u0 + iu, v0, d), t1 = at(u1 - iu, v0, d), t2 = at(u1 - iu, v1, d), t3 = at(u0 + iu, v1, d);
              for (const side of sides) {
                if (!side.shown) continue;
                if (side.edge === 0) quad(side.path, [b0, b3, t3, t0]);
                else if (side.edge === 1) quad(side.path, [b1, b2, t2, t1]);
                else if (side.edge === 2) quad(side.path, [b0, b1, t1, t0]);
                else quad(side.path, [b3, b2, t2, t3]);
              }
              quad(tops, [t0, t1, t2, t3]);
            }
            const top = face.kind === "side" ? pal.onSide : pal.end;
            for (const side of sides) {
              if (!side.shown) continue;
              c.globalAlpha = side.fade * flat;
              c.fillStyle = haze(
                dark ? mix(pal.side, "#ffffff", side.lf * 0.12) : mix(pal.onEnd, pal.outline, (1 - side.lf) * 0.26),
              );
              c.fill(side.path);
            }
            c.globalAlpha = flat;
            c.fillStyle = haze(dark ? mix(top, pal.shade, lit * pal.shadeBy) : mix(top, pal.shade, (1 - lit) * 0.06));
            c.fill(tops);
            // No hairlines on the relief: under a second of motion they
            // strobe. Its shading alone draws it.
            c.globalAlpha = 1;
          }
          if (face.kind === "side") {
            // The mark, as on the yard's drawing: upright on the wall, in its
            // middle, painted over the ribs with a keyline of the wall's
            // white around it; in the wall's own light grey, so the spiral
            // stays a background.
            // Mapped flat at its own middle, where the face is seen.
            const lift = at(a / 2, b / 2, 0.09);
            const du = at(a / 2 + 1, b / 2, 0.09);
            const dv = at(a / 2, b / 2 + 1, 0.09);
            c.setTransform(du[0] - lift[0], du[1] - lift[1], dv[0] - lift[0], dv[1] - lift[1], lift[0], lift[1]);
            const size = 1.3 / 140.7;
            c.scale(size, -size);
            c.translate(-70.35, -70.32);
            c.lineJoin = "round";
            c.lineWidth = 0.1 / size;
            c.strokeStyle = haze(pal.end);
            c.stroke(mark);
            c.fillStyle = haze(pal.side);
            c.fill(mark);
          }
          // Its edge, as fine as the relief's: no dark frame around it.
          outline();
          c.strokeStyle = edge;
          c.lineWidth = stroke * 0.5;
          c.lineJoin = "round";
          c.stroke();
        }
        for (const cb of castings) block(cb);
      }
    }

    resize();
    window.addEventListener("resize", resize);

    let frame = 0;
    let last = performance.now();
    const tick = (now: number) => {
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      // The pose follows the scroll softly; the chain keeps turning slowly.
      const target = poseAt(progress());
      const k = 1 - Math.exp(-dt * 2.6);
      for (const key of Object.keys(pose) as (keyof Pose)[]) pose[key] += (target[key] - pose[key]) * k;
      drift += dt * 0.045;
      if (window.scrollY !== lastY) way = window.scrollY > lastY ? 1 : -1;
      lastY = window.scrollY;
      idle += dt * 0.05 * way;
      spin += (idle + spinAt() - spin) * (1 - Math.exp(-dt * 4));
      draw();
      frame = requestAnimationFrame(tick);
    };
    const onScroll = () => {
      pose = poseAt(progress());
      spin = spinAt();
      draw();
    };
    if (still) {
      draw();
      window.addEventListener("scroll", onScroll, { passive: true });
    } else {
      frame = requestAnimationFrame(tick);
    }

    return () => {
      cancelAnimationFrame(frame);
      themed.disconnect();
      window.removeEventListener("resize", resize);
      window.removeEventListener("scroll", onScroll);
    };
  }, [ring]);

  return (
    <canvas
      ref={ref}
      aria-hidden="true"
      className={cn("pointer-events-none fixed inset-0 -z-10 size-full", className)}
    />
  );
}

// The container circle ("círculo de contêineres", Giovani 2026-10-08): the
// same containers in a ring around the reader, each across it like the
// blades of a fan, the eye inside, near its edge and over it. The ring turns
// as the page is scrolled, as one body, so the containers come from far
// ahead, pass by at the sides and go on behind, out of sight.
export function ContainerCircle({ className }: { className?: string }) {
  return <ContainerHelix ring className={className} />;
}
