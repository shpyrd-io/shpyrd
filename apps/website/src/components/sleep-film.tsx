"use client";

import { Bot, Moon, Sun, User, X } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { filmInset, filmPiece, useFilm } from "@/components/film";

// The film of the feature "Free while nobody uses it" (Giovani, 2026-10-19):
// the app asleep, costing nothing. Ana signs in and it wakes in seconds and
// serves her; nobody comes for a while and it sleeps again; a bot knocks and
// is turned away, the app still asleep. It ends asleep, at $0.
// Who comes is told as a vertical carousel (Giovani, 2026-10-20): each tag
// rises into its slot, and when the next comes the one before rises on and
// fades, the next rising in under it.

// Slow enough to read each step (Giovani, 2026-10-20), a little under 22s.
const MOMENTS = [
  ["asleep", 2000],
  ["ana", 1800],
  ["waking", 2400],
  ["serving", 3000],
  ["quiet", 3600],
  ["asleep-2", 1800],
  ["bot", 1800],
  ["turned-away", 2800],
  ["rest", 3000],
] as const;

export function SleepFilm() {
  const { root, name, round } = useFilm(MOMENTS);

  const awake = name === "serving" || name === "quiet";
  const waking = name === "waking";
  // Which tag is in the slot: none yet, Ana's, the bot's, then none again,
  // the bot's risen away while the app sleeps on.
  const tag = name === "asleep" ? -1 : name === "bot" || name === "turned-away" ? 1 : name === "rest" ? 2 : 0;
  const place = (i: number) =>
    i === tag ? "translate-y-0 opacity-100" : i < tag ? "-translate-y-[150%] opacity-0" : "translate-y-[150%] opacity-0";
  const pill = "absolute inset-x-0 mx-auto inline-flex w-fit items-center gap-1.5 rounded-full bg-background px-2.5 py-1 text-[10.5px] ring-1 ring-foreground/10 transition-[translate,opacity] duration-700 ease-out";

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className="flex size-full flex-col items-center justify-between gap-3">
        {/* Who comes to the app. */}
        <div className="relative h-7 w-full overflow-hidden">
          <span className={cn(pill, "top-0.5 text-foreground", place(0))}>
            <User className="size-3 text-primary" />
            ana@yourapp.com signs in
          </span>
          <span className={cn(pill, "top-0.5 text-muted-foreground", place(1))}>
            <Bot className="size-3" />
            crawler-bot
            <span className={cn("inline-flex items-center gap-0.5 overflow-hidden whitespace-nowrap text-destructive transition-[max-width,opacity] duration-300", name === "turned-away" || name === "rest" ? "max-w-24 opacity-100" : "-ml-1.5 max-w-0 opacity-0")}>
              <X className="size-3" />
              turned away
            </span>
          </span>
        </div>

        {/* The app: asleep, waking, or awake and at work. */}
        <div className={cn(filmPiece, "grid w-full gap-2.5 px-4 py-3.5 transition-[box-shadow] duration-500", awake && "ring-primary/40")}>
          <div className="flex items-center gap-2.5">
            <span
              className={cn(
                "grid size-8 place-items-center rounded-lg transition-colors duration-500",
                awake || waking ? "bg-primary/10 text-primary" : "bg-foreground/[0.06] text-muted-foreground",
              )}
            >
              {awake ? <Sun className="size-4" /> : waking ? <span className="size-3.5 animate-spin rounded-full border-2 border-primary/25 border-t-primary" /> : <Moon className="size-4" />}
            </span>
            <span className="grid leading-tight">
              <span className="font-medium text-foreground">yourapp.com</span>
              <span className="text-[10px] text-muted-foreground">
                {awake ? "Awake · 1 instance" : waking ? "Waking up… a few seconds" : "Asleep · no CPU, no memory"}
              </span>
            </span>
          </div>

          {/* What it costs right now. */}
          <div className="flex items-center justify-between rounded-md bg-foreground/[0.04] px-2.5 py-1.5">
            <span className="text-muted-foreground">Costing now</span>
            <span className={cn("text-[11px] font-semibold tabular-nums transition-colors duration-500", awake ? "text-foreground" : "text-success")}>
              {awake || waking ? "$0.012 / h" : "$0.00 / h"}
            </span>
          </div>
        </div>

        {/* Why it sleeps. */}
        <span className={cn("h-4 text-[10px] text-muted-foreground transition-opacity duration-500", name === "quiet" ? "opacity-100" : "opacity-0")}>
          No visits for 10 minutes: going to sleep
        </span>
      </div>
    </div>
  );
}
