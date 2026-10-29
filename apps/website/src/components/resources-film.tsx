"use client";

import { Check, Database, HardDrive, Zap } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { FilmPulse, filmEnter, filmInset, filmPiece, useFilm, useSize } from "@/components/film";

// The film of the feature "A database, ready to use", as Giovani told it
// (2026-10-20):
// 1. The app alone at the top, "yourapp.com", its light grey.
// 2. Under it a database comes up, is made, and is ready; a grey line draws
//    from it up to the app and an orange pulse runs up the line, as on the
//    site's other lines. Then the cache, the same; then the files.
//    Once connected, a line keeps sending pulses up until the round ends.
// 3. The app's light turns green: three connected. Then again.
// The lines meet the app at a third of it, its middle and two thirds.
// It fills its stage, 20px in from every side.

const MOMENTS = [
  ["app", 700],
  ["db", 700],
  ["db-ready", 450],
  ["db-line", 950],
  ["cache", 700],
  ["cache-ready", 450],
  ["cache-line", 950],
  ["files", 700],
  ["files-ready", 450],
  ["files-line", 950],
  ["connected", 3200],
] as const;
type Moment = (typeof MOMENTS)[number][0];

const parts = [
  { at: "db", icon: Database, name: "Database", gives: "DATABASE_URL" },
  { at: "cache", icon: Zap, name: "Cache", gives: "CACHE_URL" },
  { at: "files", icon: HardDrive, name: "Files", gives: "/data" },
] as const;

// The pieces are three columns 8px apart.
const GAP = 8;

export function ResourcesFilm() {
  const { root, past, round, still } = useFilm(MOMENTS);
  const [strip, { w, h }] = useSize<HTMLDivElement>();
  const [app, { w: appW }] = useSize<HTMLDivElement>();
  const column = (w - 2 * GAP) / 3;
  // For who asked for less motion, the end: all made, connected.
  const at = (m: Moment) => still || past(m);
  const connected = at("connected");

  // From a piece's top up to the app's foot: the path starts at the piece, so
  // the pulse runs from it to the app; the three meet the app at a third of
  // its width, its middle, and two thirds.
  const line = (i: number) => {
    const from = column / 2 + i * (column + GAP);
    const to = w / 2 + ((i - 1) * appW) / 6;
    return `M ${from} ${h} C ${from} ${h / 2}, ${to} ${h / 2}, ${to} 0`;
  };

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className="flex size-full flex-col">
        {/* The app, its light grey until all it needs is connected. */}
        <div ref={app} className={cn(filmPiece, "relative z-10 flex items-center gap-2 self-center px-3 py-2 whitespace-nowrap")}>
          <span className="relative grid size-2 place-items-center">
            {connected && !still && <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-60" />}
            <span className={cn("relative size-2 rounded-full transition-colors duration-500", connected ? "bg-success" : "bg-foreground/25")} />
          </span>
          <span className="font-medium text-foreground">yourapp.com</span>
          <span
            className={cn(
              "rounded-full bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-primary transition-opacity duration-500",
              connected ? "opacity-100" : "opacity-0",
            )}
          >
            3 connected
          </span>
        </div>

        {/* The lines from each piece up to the app: grey, drawn when the piece
            is ready, an orange pulse running up as it connects. */}
        {/* The room between the app and its pieces: all the stage leaves. */}
        <div ref={strip} className="relative -mt-2 min-h-10 flex-1">
          {w > 0 && (
            <svg aria-hidden="true" viewBox={`0 0 ${w} ${h}`} className="absolute inset-0 size-full overflow-visible">
              {parts.map((p, i) => {
                const drawn = at(`${p.at}-line` as Moment);
                return (
                  <path
                    key={p.at}
                    d={line(i)}
                    fill="none"
                    pathLength={1}
                    strokeDasharray="1 1"
                    strokeWidth={1.5}
                    className="stroke-foreground/20 transition-[stroke-dashoffset] duration-700 ease-out"
                    style={{ strokeDashoffset: drawn ? 0 : 1 }}
                  />
                );
              })}
              {/* Once a line is connected it keeps sending pulses up, each
                  line a little after the one before, until the round ends. */}
              {parts.map((p, i) =>
                !still && past(`${p.at}-line` as Moment) ? <FilmPulse key={p.at} d={line(i)} every={1600} after={0} /> : null,
              )}
            </svg>
          )}
        </div>

        {/* What it needs, each made, then ready. */}
        <div className="grid grid-cols-3" style={{ gap: GAP }}>
          {parts.map((p) => {
            const shown = at(p.at as Moment);
            const ready = at(`${p.at}-ready` as Moment);
            const Icon = p.icon;
            return (
              <div key={p.at} className="grid justify-items-center">
                <div className={cn(filmPiece, "grid w-full justify-items-center gap-1 px-1.5 py-2", shown ? filmEnter : "invisible")}>
                  <span className="grid size-7 place-items-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="size-4" />
                  </span>
                  <span className="font-medium text-foreground">{p.name}</span>
                  {ready ? (
                    <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-success animate-in fade-in-0">
                      <Check className="size-2.5" />
                      Ready
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
                      <span className="size-2.5 animate-spin rounded-full border-[1.5px] border-foreground/15 border-t-primary" />
                      Creating
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
