"use client";

import { Lock, LockKeyhole } from "lucide-react";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Button } from "@shpyrd/ui/components/button";
import { Input } from "@shpyrd/ui/components/input";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Tile } from "@shpyrd/ui/components/tile";
import { cn } from "@shpyrd/ui/lib/cn";
import { FilmFrame, filmWindow, useFilm, useTyped } from "@/components/film";

// The film of the feature "Only your people get in" (Giovani, 2026-10-19):
// the team's app in a browser, behind shpyrd's sign-in. John, of the finance
// team, signs in and the app opens for him, a mark saying who he is; Emma,
// not on its list, signs in and is kept out, told whom to ask. The app wrote
// no login code: the sign-in is shpyrd's, in front of it.

const MOMENTS = [
  ["gate", 900],
  ["type-john", 1500],
  ["check-john", 900],
  ["john-in", 3400],
  ["gate-2", 900],
  ["type-emma", 1500],
  ["check-emma", 900],
  ["emma-out", 3400],
] as const;

const expenses = [
  ["Flights · Lisbon", "$842"],
  ["Team lunch", "$126"],
  ["Laptop", "$1,980"],
  ["Hotel · 2 nights", "$410"],
  ["Taxi · airport", "$38"],
  ["Software · yearly", "$240"],
  ["Coworking · October", "$190"],
  ["Client dinner", "$212"],
] as const;

const enter = "animate-in fade-in-0 zoom-in-95 duration-normal ease-enter";

export function DoorFilm() {
  const { root, name, past, round, still } = useFilm(MOMENTS);
  const john = !past("gate-2");
  const who = john ? "john@yourapp.com" : "emma@gmail.com";
  const typing = name === "type-john" || name === "type-emma";
  const typed = useTyped(who, typing, 90);
  const checking = name === "check-john" || name === "check-emma";
  // Still, it shows John in: what the door is for.
  const inside = name === "john-in" || still;
  const out = name === "emma-out" && !still;

  return (
    <FilmFrame root={root}>
      <BrowserFrame
        address="expenses.yourapp.com"
        className={cn(
          filmWindow,
          "flex flex-col [&>div:first-child]:gap-2 [&>div:first-child]:border-foreground/6 [&>div:first-child]:bg-transparent [&>div:first-child]:px-2.5 [&>div:first-child]:py-1.5",
          "[&_[data-slot=browser-frame-address]]:bg-foreground/[0.05] [&_[data-slot=browser-frame-address]]:py-0.5 [&_[data-slot=browser-frame-address]]:text-[10px]",
          "[&_[data-slot=browser-frame-content]]:relative [&_[data-slot=browser-frame-content]]:min-h-0 [&_[data-slot=browser-frame-content]]:flex-1",
        )}
      >
        {/* The app, once John is in: his team's expenses. */}
        {inside && (
          <div key={`app-${round}`} className={cn("absolute inset-0 flex flex-col gap-2 p-3 text-[11px]", enter)}>
            <div className="flex items-center justify-between">
              <span className="font-heading text-xs font-semibold text-foreground">Expenses</span>
              <Avatar alt="John" size={20} className="bg-primary text-white" />
            </div>
            {/* As many as the window has room for; the mark of who is in stays
                at its foot. */}
            <ul className="grid min-h-0 flex-1 content-start gap-1 overflow-hidden">
              {expenses.map(([what, much], i) => (
                <li
                  key={what}
                  className="flex justify-between rounded-md bg-foreground/[0.04] px-2 py-1 text-foreground animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-both"
                  style={{ animationDelay: `${150 + i * 90}ms` }}
                >
                  <span className="truncate text-muted-foreground">{what}</span>
                  <span className="font-medium tabular-nums">{much}</span>
                </li>
              ))}
            </ul>
            {/* Who the app was told is there: shpyrd says so, the app
                wrote no sign-in of its own. */}
            <StatusBadge type="success" className="shrink-0 text-[10px] animate-in fade-in-0 fill-mode-both delay-500">
              john@yourapp.com · Finance team
            </StatusBadge>
          </div>
        )}

        {/* Kept out: not on the app's list. */}
        {out && (
          <div key={`out-${round}`} className={cn("absolute inset-0 grid place-content-center justify-items-center gap-1.5 p-4 text-center", enter)}>
            <Tile size="sm">
              <Lock />
            </Tile>
            <span className="text-xs font-semibold text-foreground">Not on the list</span>
            <span className="max-w-[16ch] text-[10.5px] leading-snug text-muted-foreground">
              Expenses is open to the Finance team. Ask its owner.
            </span>
          </div>
        )}

        {/* The sign-in shpyrd puts in front of the app. */}
        {!inside && !out && (
          <div key={`gate-${john}-${round}`} className={cn("absolute inset-0 grid place-content-center justify-items-center gap-2 p-4", enter)}>
            {/* The library's tile, field and button, as a sign-in page has them. */}
            <Tile size="sm" className="text-primary">
              <LockKeyhole />
            </Tile>
            <span className="text-xs font-semibold text-foreground">Sign in to Expenses</span>
            {/* The address typed into the field, in its middle. */}
            <Input
              size="sm"
              readOnly
              tabIndex={-1}
              aria-label="Email"
              placeholder="you@company.com"
              value={typing ? typed : checking || still ? who : ""}
              className="w-44 bg-background text-center text-xs md:text-xs"
            />
            <Button size="sm" tabIndex={-1} className={cn("w-44 transition-transform duration-150", checking && "scale-95")}>
              {checking ? (
                <span aria-label="Signing in" className="size-3.5 animate-spin rounded-full border-[1.5px] border-white/40 border-t-white" />
              ) : (
                "Continue"
              )}
            </Button>
          </div>
        )}
      </BrowserFrame>
    </FilmFrame>
  );
}
