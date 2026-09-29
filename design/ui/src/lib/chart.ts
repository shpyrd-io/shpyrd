// What the charts share: the colours a series may take, the scale of an
// axis, and the paths that are drawn. Nothing here knows of the browser.

// A chart is drawn with two colours: the ink of a line and the wash under
// it, which is the same ink, thinner. A tone says which ink.
export type Tone =
  | "orange"
  | "blue"
  | "green"
  | "violet"
  | "neutral"
  | "success"
  | "info"
  | "warning"
  | "error";

// The order series take their tone in when they name none. It is fixed:
// the fifth and the ones after it are "the others", in grey.
export const order: Tone[] = ["orange", "blue", "green", "violet", "neutral"];

export const inks: Record<Tone, string> = {
  orange: "[--ink:var(--chart-1)]",
  blue: "[--ink:var(--chart-2)]",
  green: "[--ink:var(--chart-3)]",
  violet: "[--ink:var(--chart-4)]",
  neutral: "[--ink:var(--chart-5)]",
  success: "[--ink:var(--chart-success)]",
  info: "[--ink:var(--chart-info)]",
  warning: "[--ink:var(--chart-warning)]",
  error: "[--ink:var(--chart-error)]",
};

export function toneOf(tone: Tone | undefined, index: number): Tone {
  return tone ?? order[Math.min(index, order.length - 1)];
}

// How strong the wash and the line of each series are, when every series
// has the same ink: the one drawn last, in front, is the strongest.
export function shade(index: number, count: number) {
  const at = count <= 1 ? 1 : index / (count - 1);
  return { wash: 0.1 + 0.3 * at, line: 0.45 + 0.55 * at };
}

export type Point = [time: number, value: number];

// The size of the drawing every chart is made in. It is stretched to the
// room the chart has, so nothing has to be measured.
export const WIDTH = 1000;
export const HEIGHT = 100;

// The values written along an axis: from zero, in round steps, up to the
// first one that is not under the highest value. Bytes are round in
// powers of two, and a count has no halves.
export function axis(highest: number, unit = ""): number[] {
  if (unit === "%" && highest <= 100) return [0, 25, 50, 75, 100];
  if (!Number.isFinite(highest) || highest <= 0) return [0, 1];
  const binary = unit === "bytes" || unit === "bytes/s";
  const scale = binary ? 1024 ** Math.floor(Math.log(highest) / Math.log(1024)) : 1;
  const value = highest / scale;
  const rough = value / 4;
  let step = 1;
  if (binary) {
    step = 1 / 8;
    while (step < rough) step *= 2;
  } else {
    const power = 10 ** Math.floor(Math.log10(rough));
    const rounds = unit === "count" ? [1, 2, 5, 10] : [1, 2, 2.5, 5, 10];
    step = rounds.map((r) => r * power).find((r) => r >= rough) ?? 10 * power;
    if (unit === "count") step = Math.max(1, Math.round(step));
  }
  const values = [0];
  while (values[values.length - 1] < value) {
    values.push(Number((values.length * step).toPrecision(12)));
  }
  return values.map((v) => v * scale);
}

// The time between two points of a series: the most common one.
export function stepOf(times: number[]): number {
  if (times.length < 2) return 60;
  const gaps = times.slice(1).map((t, i) => t - times[i]).sort((a, b) => a - b);
  return gaps[Math.floor(gaps.length / 2)] || 60;
}

export type Scale = { from: number; to: number; max: number };

export function x(time: number, scale: Scale): number {
  return ((time - scale.from) / (scale.to - scale.from || 1)) * WIDTH;
}

export function y(value: number, scale: Scale): number {
  return HEIGHT - (Math.min(value, scale.max) / (scale.max || 1)) * HEIGHT;
}

const n = (v: number) => Number(v.toFixed(2));

// The top edge of a series. In steps, a value holds until the next one:
// what was measured is for a stretch of time, not for an instant.
export function edge(
  points: Point[],
  scale: Scale,
  kind: "step" | "line",
  end: number,
): string {
  if (points.length === 0) return "";
  const parts: string[] = [];
  points.forEach(([time, value], i) => {
    const px = n(x(time, scale));
    const py = n(y(value, scale));
    if (i === 0) parts.push(`M${px},${py}`);
    else if (kind === "step") parts.push(`H${px}`, `V${py}`);
    else parts.push(`L${px},${py}`);
  });
  if (kind === "step") parts.push(`H${n(x(end, scale))}`);
  return parts.join("");
}

// The same edge, closed down to another edge or to the bottom of the
// chart: what the wash fills.
export function area(
  points: Point[],
  scale: Scale,
  kind: "step" | "line",
  end: number,
  under?: Point[],
): string {
  if (points.length === 0) return "";
  const top = edge(points, scale, kind, end);
  const last = kind === "step" ? end : points[points.length - 1][0];
  if (!under || under.length === 0) {
    return `${top}V${HEIGHT}H${n(x(points[0][0], scale))}Z`;
  }
  // Back along the edge under it, from the end to the start.
  const back: string[] = [];
  const reversed = [...under].reverse();
  reversed.forEach(([time, value], i) => {
    const px = n(x(time, scale));
    const py = n(y(value, scale));
    if (i === 0) {
      back.push(`L${n(x(last, scale))},${py}`);
      if (kind === "step") back.push(`H${px}`);
      else back.push(`L${px},${py}`);
    } else if (kind === "step") {
      back.push(`V${py}`, `H${px}`);
    } else {
      back.push(`L${px},${py}`);
    }
  });
  return `${top}${back.join("")}Z`;
}
