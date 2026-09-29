import * as React from "react";
import { cn } from "cn";
import { inks, type Tone } from "../lib/chart";

// The marks a meter is drawn with. What is used is filled; what is
// reserved and not used is hatched; what is free is only outlined. The
// three are told apart by how they are drawn, not by their colour.
const marks = {
  used: "border-(--ink) bg-(--ink)/25",
  reserved:
    "border-(--ink)/60 bg-[repeating-linear-gradient(135deg,color-mix(in_oklab,var(--ink)_70%,transparent)_0_1px,transparent_1px_5px)]",
  free: "border-border bg-transparent",
} as const;

// How much of something is taken, against how much there is of it: what
// is used, what is reserved, and the capacity.
function Meter({
  className,
  label,
  unit,
  icon,
  used,
  reserved,
  capacity,
  tone,
  warnAt = 75,
  dangerAt = 90,
  format = (value) => value.toLocaleString("en"),
  ...props
}: React.ComponentProps<"div"> & {
  label: string;
  // What the values are counted in, beside the label: `cores`, `GiB`.
  unit?: string;
  icon?: React.ReactElement;
  used: number;
  // What was set aside, used or not. It may be left out.
  reserved?: number;
  capacity: number;
  // The ink. Without it, it follows how much is used.
  tone?: Tone;
  // The percentages from which the ink is that of a warning, of an error.
  warnAt?: number;
  dangerAt?: number;
  format?: (value: number) => string;
}) {
  const share = (value: number) =>
    capacity > 0 ? Math.min(100, Math.max(0, (value / capacity) * 100)) : 0;
  const percent = capacity > 0 ? (used / capacity) * 100 : 0;
  const ink =
    tone ?? (percent >= dangerAt ? "error" : percent >= warnAt ? "warning" : "orange");
  const [whole, part] = percent.toFixed(1).split(".");
  const over = reserved !== undefined && reserved > used ? share(reserved) - share(used) : 0;
  const free = 100 - share(used) - over;

  const rows = [
    { name: "Used", value: used, mark: marks.used },
    ...(reserved === undefined
      ? []
      : [{ name: "Reserved", value: reserved, mark: marks.reserved }]),
    { name: "Capacity", value: capacity, mark: marks.free },
  ];

  return (
    <div
      data-slot="meter"
      data-tone={ink}
      className={cn("@container/meter grid gap-3 text-sm", inks[ink], className)}
      {...props}
    >
      <div className="flex flex-wrap items-end justify-between gap-x-3 gap-y-2">
        <div className="flex min-w-0 items-center gap-2 font-medium [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-(--ink)">
          {icon}
          <span className="truncate">{label}</span>
          {unit && <span className="font-normal text-muted-foreground">{unit}</span>}
        </div>
        <div className="font-heading leading-none whitespace-nowrap">
          <span className="text-3xl font-medium">{whole}</span>
          <span className="text-base text-muted-foreground">.{part}%</span>
        </div>
      </div>

      <div
        role="meter"
        aria-label={`${label}: ${format(used)} of ${format(capacity)} used`}
        aria-valuemin={0}
        aria-valuemax={capacity}
        aria-valuenow={used}
        className="flex h-3 gap-0.5"
      >
        {share(used) > 0 && (
          <span
            title={`Used: ${format(used)}`}
            className={cn("min-w-1 rounded-[3px] border", marks.used)}
            style={{ width: `${share(used)}%` }}
          />
        )}
        {over > 0 && (
          <span
            title={`Reserved: ${format(reserved as number)}`}
            className={cn("min-w-1 rounded-[3px] border", marks.reserved)}
            style={{ width: `${over}%` }}
          />
        )}
        {free > 0 && (
          <span
            title={`Free: ${format(Math.max(0, capacity - Math.max(used, reserved ?? 0)))}`}
            className={cn("min-w-1 rounded-[3px] border", marks.free)}
            style={{ width: `${free}%` }}
          />
        )}
      </div>

      {/* With little room, a line for each; with more, side by side. */}
      <dl className="grid gap-1 @xs/meter:flex @xs/meter:justify-between @xs/meter:gap-4">
        {rows.map((row) => (
          <div
            key={row.name}
            className="flex items-center justify-between gap-3 @xs/meter:grid @xs/meter:justify-normal @xs/meter:gap-0.5"
          >
            <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <span
                aria-hidden="true"
                className={cn("size-2.5 shrink-0 rounded-[3px] border", row.mark)}
              />
              {row.name}
            </dt>
            <dd className="font-mono text-xs tabular-nums">{format(row.value)}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

export { Meter };
