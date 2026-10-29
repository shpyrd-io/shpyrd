"use client";

import { Database, HardDrive, History, Users, Zap } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { filmEnter, filmInset, filmPiece, useFilm } from "@/components/film";

// The film of the feature "Everything about it, on one page" (Giovani,
// 2026-10-19): the app's page putting itself together, one part after the
// other, as the copy says: which version is online, what it uses, who can
// change it, and what they did. As the other cards (2026-10-20): the app is
// yourapp.com, what it uses is named as card 3 names it (Database, Cache,
// Files), the people are those of yourapp.com, and each part holds long
// enough to be read.

const MOMENTS = [
  ["head", 1400],
  ["uses", 1400],
  ["people", 1400],
  ["log-1", 1300],
  ["log-2", 1300],
  ["rest", 3600],
] as const;

const uses = [
  { Icon: Database, label: "Database" },
  { Icon: Zap, label: "Cache" },
  { Icon: HardDrive, label: "Files" },
] as const;

const people = [
  ["A", "bg-primary text-white", "ana@yourapp.com"],
  ["J", "bg-foreground/80 text-background", "john@yourapp.com"],
  ["M", "bg-foreground/[0.12] text-foreground", "mia@yourapp.com"],
] as const;

export function OverviewFilm() {
  const { root, past, round } = useFilm(MOMENTS);

  return (
    <div ref={root} className={filmInset}>
      <div key={round} className={cn(filmPiece, "flex size-full flex-col gap-2.5 px-4 py-3.5")}>
        {/* Which version is online. */}
        <div className="flex items-center justify-between">
          <span className="font-heading text-sm font-semibold text-foreground">yourapp.com</span>
          <span className="inline-flex items-center gap-1.5 rounded-full bg-success/12 px-2 py-0.5 text-[10px] font-medium text-success">
            <span className="size-1.5 rounded-full bg-success" />
            Online · v41
          </span>
        </div>

        {/* What it uses. */}
        {past("uses") && (
          <div className={cn("flex items-center gap-1.5", filmEnter)}>
            {uses.map(({ Icon, label }) => (
              <span key={label} className="inline-flex items-center gap-1 rounded-md bg-foreground/[0.05] px-1.5 py-1 text-[10px] text-foreground">
                <Icon className="size-3 text-primary" />
                {label}
              </span>
            ))}
          </div>
        )}

        {/* Who can change it. */}
        {past("people") && (
          <div className={cn("flex items-center justify-between", filmEnter)}>
            <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
              <Users className="size-3" />
              Can change it
            </span>
            <span className="flex -space-x-1.5">
              {people.map(([initial, tone, email]) => (
                <span key={initial} title={email} className={cn("grid size-5 place-items-center rounded-full text-[9px] font-semibold ring-2 ring-background", tone)}>
                  {initial}
                </span>
              ))}
            </span>
          </div>
        )}

        {/* What they did. */}
        {past("log-1") && (
          <div className="mt-auto grid gap-1 border-t border-foreground/8 pt-2">
            <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
              <History className="size-3" />
              Activity
            </span>
            <span className={cn("flex justify-between text-[10.5px] text-foreground", filmEnter)}>
              <span>
                <b className="font-medium">ana@yourapp.com</b> rolled back to v41
              </span>
              <span className="text-muted-foreground">2 min</span>
            </span>
            {past("log-2") && (
              <span className={cn("flex justify-between text-[10.5px] text-foreground", filmEnter)}>
                <span>
                  <b className="font-medium">john@yourapp.com</b> released v42
                </span>
                <span className="text-muted-foreground">1 h</span>
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
