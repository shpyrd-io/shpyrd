import * as React from "react";
import { cn } from "cn";
import { inks, toneOf, type Tone } from "../lib/chart";

// How far something has gone, or of what parts it is made. It is drawn as
// a Meter is: what is done is filled, what is left is only outlined.

export type Segment = { value: number; label?: string; tone?: Tone };

const heights = { sm: "h-2", default: "h-3", lg: "h-4" } as const;

function ProgressBar({
  className,
  label,
  value,
  segments,
  max = 100,
  tone = "orange",
  size = "default",
  inline = false,
  format = (value) => value.toLocaleString("en"),
  ...props
}: React.ComponentProps<"div"> & {
  // Read aloud, and written over the bar when the bar is not inline.
  label?: string;
  // One part, or several: each with its ink. One of the two is given.
  value?: number;
  segments?: Segment[];
  max?: number;
  // The ink of a single value.
  tone?: Tone;
  size?: keyof typeof heights;
  // In a line of text, beside a word.
  inline?: boolean;
  format?: (value: number) => string;
}) {
  const parts: Segment[] =
    segments ?? (value === undefined ? [] : [{ value, tone }]);
  const share = (v: number) => (max > 0 ? Math.min(100, Math.max(0, (v / max) * 100)) : 0);
  const total = parts.reduce((sum, p) => sum + Math.max(0, p.value), 0);
  const done = share(total);
  const legend = parts.some((p) => p.label !== undefined);

  // A span, so that it may sit in a line of text.
  const bar = (
    <span
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.min(max, total)}
      aria-valuetext={`${format(Math.min(max, total))} of ${format(max)}`}
      className={cn("flex w-full gap-0.5", heights[size])}
    >
      {parts.map((part, i) =>
        share(part.value) > 0 ? (
          <span
            key={i}
            title={part.label && `${part.label}: ${format(part.value)}`}
            className={cn(
              "min-w-1 rounded-[3px] border border-(--ink) bg-(--ink)/25",
              inks[toneOf(part.tone, i)],
            )}
            style={{ width: `${share(part.value)}%` }}
          />
        ) : null,
      )}
      {done < 100 && (
        <span
          className="min-w-1 rounded-[3px] border border-border bg-transparent"
          style={{ width: `${100 - done}%` }}
        />
      )}
    </span>
  );

  if (inline) {
    return (
      <span
        data-slot="progress-bar"
        data-inline
        className={cn("inline-flex w-24 align-middle", className)}
        {...(props as React.ComponentProps<"span">)}
      >
        {bar}
      </span>
    );
  }

  return (
    <div data-slot="progress-bar" className={cn("grid gap-1.5 text-sm", className)} {...props}>
      {label && (
        <div className="flex items-end justify-between gap-3">
          <span className="min-w-0 truncate font-medium">{label}</span>
          <span className="font-mono text-xs text-muted-foreground tabular-nums">
            {Math.round(done)}%
          </span>
        </div>
      )}
      {bar}
      {legend && (
        <dl className="flex flex-wrap gap-x-4 gap-y-1">
          {parts.map((part, i) => (
            <div key={i} className={cn("flex items-center gap-1.5", inks[toneOf(part.tone, i)])}>
              <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span
                  aria-hidden="true"
                  className="size-2.5 shrink-0 rounded-[3px] border border-(--ink) bg-(--ink)/25"
                />
                {part.label}
              </dt>
              <dd className="font-mono text-xs tabular-nums">{format(part.value)}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}

export { ProgressBar };
