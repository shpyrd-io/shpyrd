import type { Point, Series } from "@/api/types";

// What the Mock draws along the time: a line that wanders around a base,
// the same every time for the same seed.
function random(seed: number) {
  let s = seed >>> 0 || 1;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}

export function wander(seed: number, to: number, { points = 60, step = 60, base = 50, swing = 0.2 } = {}): Point[] {
  const next = random(seed);
  let v = base;
  return Array.from({ length: points }, (_, i): Point => {
    v = Math.max(0, base + (v - base) * 0.8 + (next() - 0.5) * base * swing);
    return [to - (points - i) * step, Math.round(v * 100) / 100];
  });
}

const tones: Series["tone"][] = ["orange", "blue", "green", "violet"];

// A series per node, in percent, for the given range.
export function byNode(nodes: { name: string; base: number }[], range: string, salt: number): Series[] {
  const to = Math.floor(Date.now() / 1000 / 60) * 60;
  const step = range === "7d" ? 60 * 168 : range === "24h" ? 60 * 24 : range === "6h" ? 360 : 60;
  return nodes.map((n, i) => ({
    name: n.name,
    tone: tones[i % tones.length],
    points: wander(salt + i * 7 + [...n.name].reduce((h, c) => h + c.charCodeAt(0), 0), to, { step, base: n.base }),
  }));
}
