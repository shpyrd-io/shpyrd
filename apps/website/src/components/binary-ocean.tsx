"use client";

import * as React from "react";

// The binary ocean from the launch film (Aprendendo Remotion,
// src/Shpyrd/OceanoBinario.tsx), drawn live: a sea of 0s and 1s seen almost
// level with the water, the far digits gathering into a bright line at the
// horizon, crossing waves moving slowly, light falling from above. It fills
// the box it is put in, follows the theme, rests when it is out of sight, and
// holds still for who asked the system for less motion.

const themes = {
  dark: { ink: "#ff7a3d", alpha: 0.85, blend: "lighter", glowA: 0.5, rayA: 0.035, vignette: true },
  light: { ink: "#6e737c", alpha: 0.95, blend: "source-over", glowA: 0.14, rayA: 0.02, vignette: false },
} as const;

const GLOW = "255,79,0";
// An alpha as a colour accepts it: never in exponent notation.
const rgba = (a: number) => `rgba(${GLOW},${Math.max(0, a).toFixed(4)})`;
const LOOP = 390; // frames in one cycle, at 30 a second: 13s
const FPS = 24;
const COLS = 120; // fewer than the film (150 × 170): drawn live, every frame costs
const ROWS = 130;
const X_RANGE = 46;
const Z_NEAR = 4;
const Z_FAR = 90;
const FLIP_PERIODS = [30, 39, 65, 78, 130, 195];
const TAU = Math.PI * 2;
const HIGHEST = 1.3 + 0.8 + 0.45 + 0.25;

const hash = (n: number) => {
  const x = Math.sin(n * 12.9898) * 43758.5453;
  return x - Math.floor(x);
};
const smooth = (e0: number, e1: number, x: number) => {
  const t = Math.min(1, Math.max(0, (x - e0) / (e1 - e0)));
  return t * t * (3 - 2 * t);
};
const height = (x: number, z: number, ph: number) =>
  1.3 * Math.sin(0.26 * x + 0.16 * z - ph) +
  0.8 * Math.sin(-0.18 * x + 0.3 * z + 2 * ph + 1.3) +
  0.45 * Math.sin(0.44 * x - 0.12 * z - 2 * ph + 2.1) +
  0.25 * Math.sin(0.1 * x + 0.6 * z + 3 * ph + 0.4);

function draw(
  ctx: CanvasRenderingContext2D,
  W: number,
  H: number,
  dpr: number,
  frame: number,
  dark: boolean,
  horizonAt: number,
) {
  // Drawn in CSS pixels, scaled to the screen's pixels.
  const place = (a: number, d: number, e: number, f: number) => ctx.setTransform(a * dpr, 0, 0, d * dpr, e * dpr, f * dpr);
  const t = dark ? themes.dark : themes.light;
  const s = H / 1080;
  const cx = W / 2;
  const horizon = H * horizonAt;
  // The width decides how far the sea reaches across, so a wide, short box
  // still shows the whole of it.
  const focal = Math.max(1000 * s, W * 0.55);
  const ph = (frame / LOOP) * TAU;
  const camH = HIGHEST + 0.6 + 1.2 * (0.5 - 0.5 * Math.cos(ph));

  place(1, 1, 0, 0);
  ctx.globalCompositeOperation = "source-over";
  ctx.globalAlpha = 1;
  ctx.clearRect(0, 0, W, H);

  const glow = ctx.createRadialGradient(cx, -H * 0.08, 0, cx, -H * 0.08, H * 0.85);
  glow.addColorStop(0, rgba(t.glowA));
  glow.addColorStop(0.45, rgba(t.glowA * 0.25));
  glow.addColorStop(1, rgba(0));
  ctx.fillStyle = glow;
  ctx.fillRect(0, 0, W, H);

  ctx.save();
  ctx.filter = `blur(${10 * s}px)`;
  for (let r = 0; r < 9; r++) {
    const spread = (r - 4) * 0.09 + 0.02 * Math.sin(ph + r);
    const topW = 26 * s * (0.6 + hash(r));
    const a = t.rayA * (0.5 + 0.5 * Math.sin(ph * 2 + r * 1.7)) * (1 - Math.abs(r - 4) / 6);
    const g = ctx.createLinearGradient(0, 0, 0, horizon);
    g.addColorStop(0, rgba(0));
    g.addColorStop(0.5, rgba(a));
    g.addColorStop(1, rgba(a * 1.6));
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.moveTo(cx + spread * W - topW, 0);
    ctx.lineTo(cx + spread * W + topW, 0);
    ctx.lineTo(cx + spread * W * 0.15 + 3 * s, horizon);
    ctx.lineTo(cx + spread * W * 0.15 - 3 * s, horizon);
    ctx.closePath();
    ctx.fill();
  }
  ctx.restore();

  ctx.save();
  ctx.translate(cx, horizon);
  ctx.scale(1, 0.18);
  const halo = ctx.createRadialGradient(0, 0, 0, 0, 0, W * 0.32);
  halo.addColorStop(0, rgba(t.glowA * 0.55));
  halo.addColorStop(1, rgba(0));
  ctx.fillStyle = halo;
  ctx.fillRect(-W, -H * 3, W * 2, H * 6);
  ctx.restore();

  ctx.globalCompositeOperation = t.blend;
  ctx.fillStyle = t.ink;
  ctx.font = `700 10px "Geist Mono Variable", ui-monospace, monospace`;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";

  for (let j = 0; j < ROWS; j++) {
    const z = Z_NEAR + (j / (ROWS - 1)) * (Z_FAR - Z_NEAR);
    const fog = smooth(Z_FAR, Z_FAR * 0.8, z);
    const nearFade = smooth(Z_NEAR, Z_NEAR + 2.5, z);
    for (let i = 0; i < COLS; i++) {
      const x = -X_RANGE + (i / (COLS - 1)) * 2 * X_RANGE;
      const y = height(x, z, ph);
      const sx = cx + (focal * x) / z;
      if (sx < -20 || sx > W + 20) continue;
      const sy = horizon + (focal * (camH - y)) / z;
      if (sy < -20 || sy > H + 20) continue;

      const side = smooth(X_RANGE, X_RANGE * 0.7, Math.abs(x));
      const crest = 0.45 + 0.55 * smooth(-2, 2.2, y);
      const a = t.alpha * fog * nearFade * side * crest;
      if (a < 0.01) continue;

      const k = j * COLS + i;
      const period = FLIP_PERIODS[Math.floor(hash(k) * FLIP_PERIODS.length)];
      const step = Math.floor((frame + hash(k + 0.5) * period) / period);
      const bit = hash(k * 7.31 + step * 1.17) > 0.5 ? "1" : "0";

      const scale = (Math.max(s, focal / 1000) * 7.5) / z;
      ctx.globalAlpha = a;
      place(scale, scale, sx, sy);
      ctx.fillText(bit, 0, 0);
    }
  }

  if (t.vignette) {
    place(1, 1, 0, 0);
    ctx.globalCompositeOperation = "source-over";
    ctx.globalAlpha = 1;
    const v = ctx.createRadialGradient(cx, H * 0.45, H * 0.35, cx, H * 0.45, W * 0.75);
    v.addColorStop(0, "rgba(0,0,0,0)");
    v.addColorStop(1, "rgba(0,0,0,0.4)");
    ctx.fillStyle = v;
    ctx.fillRect(0, 0, W, H);
  }
}

export function BinaryOcean({
  className,
  // Where the horizon sits, as a share of the height.
  horizon = 0.66,
}: {
  className?: string;
  horizon?: number;
}) {
  const ref = React.useRef<HTMLCanvasElement>(null);

  React.useEffect(() => {
    const canvas = ref.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;

    const still = window.matchMedia("(prefers-reduced-motion: reduce)");
    let W = 0;
    let H = 0;
    let frame = 0;
    let last = 0;
    let raf = 0;
    let visible = true;

    const isDark = () => document.documentElement.classList.contains("dark");
    const paint = () => draw(ctx, W, H, dpr, frame, isDark(), horizon);

    let dpr = 1;

    const size = () => {
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      const box = canvas.getBoundingClientRect();
      W = Math.max(1, Math.round(box.width));
      H = Math.max(1, Math.round(box.height));
      canvas.width = W * dpr;
      canvas.height = H * dpr;
      paint();
    };

    const tick = (now: number) => {
      raf = requestAnimationFrame(tick);
      if (!visible || still.matches) return;
      if (now - last < 1000 / FPS) return;
      last = now;
      frame = (frame + 1) % LOOP;
      paint();
    };

    const resize = new ResizeObserver(size);
    resize.observe(canvas);
    const seen = new IntersectionObserver(([entry]) => (visible = entry.isIntersecting));
    seen.observe(canvas);
    const recolour = new MutationObserver(paint);
    recolour.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    document.fonts?.load(`700 10px "Geist Mono Variable"`).then(paint, () => {});

    size();
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      resize.disconnect();
      seen.disconnect();
      recolour.disconnect();
    };
  }, [horizon]);

  return <canvas ref={ref} aria-hidden="true" className={className} />;
}
