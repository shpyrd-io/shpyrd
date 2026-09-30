import * as React from "react";
import { cn } from "cn";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { inks, type Tone } from "../lib/chart";

// How a value went lately, as small as a word: a line and its wash, no
// axis and no numbers. What it says in numbers is beside it.
function Sparkline({
  className,
  values,
  tone = "orange",
  kind = "step",
  ...props
}: Omit<React.ComponentProps<"span">, "children"> & {
  values: number[];
  tone?: Tone;
  kind?: "step" | "line";
}) {
  const max = Math.max(...values, 0) || 1;
  const width = 100;
  const height = 30;
  const count = kind === "step" ? values.length : values.length - 1;
  const x = (i: number) => Number(((i / (count || 1)) * width).toFixed(2));
  // The top is kept a little under the edge, so the line is not cut.
  const y = (v: number) => Number((height - 1 - (v / max) * (height - 2)).toFixed(2));
  const edge = values
    .map((v, i) =>
      i === 0
        ? `M0,${y(v)}`
        : kind === "step"
          ? `H${x(i)}V${y(v)}`
          : `L${x(i)},${y(v)}`,
    )
    .join("")
    .concat(kind === "step" ? `H${width}` : "");
  return (
    <span
      data-slot="sparkline"
      className={cn("relative block h-8 w-24", inks[tone], className)}
      {...props}
    >
      {values.length > 0 && (
        <svg
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="none"
          className="absolute inset-0 size-full overflow-visible"
          aria-hidden="true"
        >
          <path d={`${edge}V${height}H0Z`} className="fill-(--ink)" fillOpacity={0.14} />
          <path
            d={edge}
            fill="none"
            vectorEffect="non-scaling-stroke"
            strokeWidth={1.5}
            strokeLinejoin="round"
            className="stroke-(--ink)"
          />
        </svg>
      )}
    </span>
  );
}

// A number that matters, by itself: what it is, how much, how it changed
// and how it went lately.
function Stat({
  className,
  label,
  value,
  unit,
  delta,
  trend,
  tone = "orange",
  ...props
}: React.ComponentProps<"div"> & {
  label: React.ReactNode;
  value: React.ReactNode;
  // What the value is counted in, smaller, after it.
  unit?: React.ReactNode;
  // How it changed: the change, against what, and whether it is good.
  delta?: { value: string; direction: "up" | "down"; good?: boolean; against?: string };
  trend?: number[];
  tone?: Tone;
}) {
  return (
    <div data-slot="stat" className={cn("grid gap-1", className)} {...props}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="flex items-end justify-between gap-3">
        <div className="font-heading leading-none">
          <span className="text-2xl font-medium">{value}</span>
          {unit && <span className="ml-1 text-sm text-muted-foreground">{unit}</span>}
        </div>
        {trend && <Sparkline values={trend} tone={tone} />}
      </div>
      {delta && (
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <span
            className={cn(
              "inline-flex items-center gap-0.5 font-medium [&_svg]:size-3.5",
              delta.good === undefined
                ? "text-foreground"
                : delta.good
                  ? "text-success"
                  : "text-destructive",
            )}
          >
            {delta.direction === "up" ? <ArrowUpRight /> : <ArrowDownRight />}
            {delta.value}
          </span>
          {delta.against}
        </div>
      )}
    </div>
  );
}

export { Sparkline, Stat };
