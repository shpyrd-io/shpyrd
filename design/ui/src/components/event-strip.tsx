"use client";

import * as React from "react";
import { cn } from "cn";
import { inks, type Tone } from "../lib/chart";
import { timeLabel } from "../lib/format";

export type StripEvent = {
  time: number;
  // How many there were in that stretch of time. One, when left out.
  count?: number;
  // What is told of it when pointed at.
  title?: string;
};

export type StripRow = {
  label: string;
  tone?: Tone;
  events: StripEvent[];
};

const never = () => () => {};

// What happened and when, a row for each kind of thing: errors, releases,
// restarts. Each mark is a moment; the more there were in it, the
// stronger it is drawn. It is as wide as a time chart and has the same
// room at its left, so the two can share the time.
function EventStrip({
  className,
  title,
  rows,
  from,
  to,
  range = "1h",
  ...props
}: Omit<React.ComponentProps<"figure">, "title"> & {
  title?: React.ReactNode;
  rows: StripRow[];
  // The stretch of time shown, in seconds.
  from: number;
  to: number;
  range?: string;
}) {
  // Times are written in the language and the hours of who reads them.
  const browser = React.useSyncExternalStore(
    never,
    () => true,
    () => false,
  );
  const left = (time: number) => `${((time - from) / (to - from || 1)) * 100}%`;
  const most = Math.max(1, ...rows.flatMap((r) => r.events.map((e) => e.count ?? 1)));
  const labels = Array.from({ length: 5 }, (_, i) => from + ((to - from) * i) / 4);

  return (
    <figure
      data-slot="event-strip"
      className={cn("@container/chart grid gap-3", className)}
      {...props}
    >
      {title && <figcaption className="text-sm leading-snug font-medium">{title}</figcaption>}
      {/* The columns of a time chart: the room at the left, the drawing,
          and at the right what the chart gives to its legend. */}
      <div className="grid grid-cols-[3.5rem_minmax(0,1fr)] items-center gap-y-1 @xl/chart:grid-cols-[3.5rem_minmax(0,1fr)_9.5rem]">
        {rows.map((row) => {
          const total = row.events.reduce((sum, e) => sum + (e.count ?? 1), 0);
          return (
            <React.Fragment key={row.label}>
              <span className="truncate pr-2 text-right font-mono text-[11px] text-muted-foreground">
                {row.label}
              </span>
              <div
                className={cn(
                  "relative h-5 border-b border-border",
                  inks[row.tone ?? "neutral"],
                )}
              >
                {row.events.map((event) => {
                  const count = event.count ?? 1;
                  const strength = 0.25 + 0.75 * (count / most);
                  const what =
                    event.title ?? `${row.label}${count > 1 ? `, ${count} times` : ""}`;
                  return (
                    <span
                      key={event.time}
                      title={browser ? `${what}, ${timeLabel(event.time, range)}` : what}
                      className="absolute bottom-1 h-3 w-1 -translate-x-1/2 rounded-[2px] border border-(--ink)"
                      style={{ left: left(event.time) }}
                    >
                      <span
                        className="absolute inset-0 bg-(--ink)"
                        style={{ opacity: strength }}
                      />
                    </span>
                  );
                })}
              </div>
              <span
                aria-hidden="true"
                className="hidden pl-6 font-mono text-[11px] tabular-nums @xl/chart:block"
              >
                {total === 0 ? <span className="text-muted-foreground">—</span> : total}
              </span>
            </React.Fragment>
          );
        })}
        <div className="relative col-start-2 mt-1 h-4">
          {labels.map((t, i) => (
            <span
              key={t}
              className={cn(
                "absolute font-mono text-[10px] whitespace-nowrap text-muted-foreground tabular-nums",
                i === 0 ? "" : i === labels.length - 1 ? "-translate-x-full" : "-translate-x-1/2",
              )}
              style={{ left: left(t) }}
            >
              {browser && timeLabel(t, range)}
            </span>
          ))}
        </div>
      </div>
      {/* How many there were of each. With room it is read at the end of
          each row; without, here, as the legend of a chart is. */}
      <ul className="flex flex-wrap gap-x-5 gap-y-1 pl-14 @xl/chart:sr-only">
        {rows.map((row) => (
          <li
            key={row.label}
            className={cn("flex items-center gap-1.5 text-[11px]", inks[row.tone ?? "neutral"])}
          >
            <span
              aria-hidden="true"
              className="h-3 w-1 rounded-[2px] border border-(--ink) bg-(--ink)/60"
            />
            <span className="text-muted-foreground">{row.label}</span>
            <span className="font-mono font-medium tabular-nums">
              {row.events.reduce((sum, e) => sum + (e.count ?? 1), 0)}
            </span>
          </li>
        ))}
      </ul>
    </figure>
  );
}

export { EventStrip };
