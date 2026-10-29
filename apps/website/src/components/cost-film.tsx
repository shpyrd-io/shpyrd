"use client";

import { cn } from "@shpyrd/ui/lib/cn";
import { filmInset, filmPiece, useFilm } from "@/components/film";

// The film of the feature "Pay only for what it uses" (Giovani, 2026-10-19;
// blocks instead of a line, 2026-10-20, so it reads apart from the health
// chart): an agent's CPU over a while as stacked blocks, the way an AI's
// token use is drawn. Each column is a moment; its faint outline is what a
// server kept on would hold for it all along, its orange blocks what the
// agent used: one small block while it waits for the AI, a full column when
// it works. The columns fill from left to right; beside them two meters, what
// it costs on shpyrd, moving only with the orange, and what the server kept
// on would cost, moving all the time. All in the site's own font.

// Many small blocks, for the look of many tokens (Giovani): about 8px.
const COLUMNS = 44;
const STACK = 9;
// The moments it works, and how hard; the rest of the time one block.
const work: Record<number, number> = {
  5: 3, 6: 7, 7: 9, 8: 6, 9: 2,
  17: 4, 18: 8, 19: 9, 20: 5,
  29: 3, 30: 6, 31: 9, 32: 7, 33: 2,
  39: 5, 40: 8, 41: 4,
};
const used = Array.from({ length: COLUMNS }, (_, i) => work[i] ?? 1);

const STEPS = 18;
const MOMENTS = [
  ...Array.from({ length: STEPS }, (_, i) => [`t${i}`, 340] as const),
  ["rest", 2600],
] as const;

// What each meter has counted once a number of columns are in.
const ours = (columns: number) => used.slice(0, columns).reduce((a, v) => a + (v > 1 ? 0.0001 * v : 0.00002), 0);
const kept = (columns: number) => (columns / COLUMNS) * 0.09;

export function CostFilm() {
  const { root, name, round, still } = useFilm(MOMENTS);
  const step = name === "rest" ? STEPS : Number(name.slice(1)) + 1;
  const columns = still ? COLUMNS : Math.round((step / STEPS) * COLUMNS);

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className={cn(filmPiece, "flex size-full flex-col gap-3 px-4 py-3.5")}>
        <div className="flex items-baseline justify-between gap-2">
          <span className="font-medium text-foreground">agent · worker</span>
          <span className="truncate text-[10px] text-muted-foreground">CPU, mostly waiting for the AI</span>
        </div>

        {/* The CPU as stacked blocks, filled as far as the film has gone. */}
        <div className="flex min-h-12 flex-1 items-end gap-[2px]">
          {used.map((n, i) => (
            <div key={i} className="flex h-full min-w-0 flex-1 flex-col-reverse gap-[2px]">
              {Array.from({ length: STACK }, (_, b) => {
                const on = i < columns && b < n;
                return (
                  <span
                    key={b}
                    className={cn(
                      "min-h-0 flex-1 rounded-[1px] transition-colors duration-300",
                      on ? (n > 1 ? "bg-primary" : "bg-primary/45") : "bg-foreground/[0.06]",
                    )}
                  />
                );
              })}
            </div>
          ))}
        </div>

        {/* The two meters. */}
        <div className="grid grid-cols-2 gap-2">
          {/* Each on one line: what it is, small, and what it costs, larger. */}
          <div className="flex items-baseline justify-between gap-2 rounded-md bg-primary/[0.07] px-2.5 py-1.5">
            <span className="truncate text-[10px] text-muted-foreground">On shpyrd, by use</span>
            <span className="text-sm font-semibold text-primary tabular-nums">${ours(columns).toFixed(4)}</span>
          </div>
          <div className="flex items-baseline justify-between gap-2 rounded-md bg-foreground/[0.04] px-2.5 py-1.5">
            <span className="truncate text-[10px] text-muted-foreground">A server kept on</span>
            <span className="text-sm font-semibold text-foreground/70 tabular-nums">${kept(columns).toFixed(4)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
