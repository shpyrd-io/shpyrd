"use client";

// A company's apps, at first scattered and unguarded (a link here, a
// password there), then gathered in one place behind one lock, and again,
// turn by turn. The first slide of the Small Software manifesto, and its
// picture on the home page. Gathered and still for who asked for less motion.
import { useEffect, useState } from "react";
import {
  CalendarClock, FileSignature, Lock, LockOpen, Megaphone, Package, Percent, Receipt, ShieldCheck, Smile, TrendingUp, UserPlus,
} from "lucide-react";
import { Tile } from "@shpyrd/ui/components/tile";
import { cn } from "@shpyrd/ui/lib/cn";
import { apps, opening } from "@shpyrd/content/site/tour";
import { panel } from "./parts";

// Each app's symbol and colour on its card.
const looks: Record<string, [React.ReactElement, string]> = {
  expenses: [<Receipt key="r" />, "text-chart-1"],
  onboarding: [<UserPlus key="u" />, "text-chart-4"],
  pipeline: [<TrendingUp key="t" />, "text-chart-2"],
  "nps-board": [<Smile key="s" />, "text-chart-3"],
  campaigns: [<Megaphone key="m" />, "text-chart-1"],
  contracts: [<FileSignature key="f" />, "text-chart-4"],
  inventory: [<Package key="p" />, "text-chart-2"],
  commissions: [<Percent key="c" />, "text-chart-3"],
  shifts: [<CalendarClock key="k" />, "text-muted-foreground"],
};
// Asleep on shpyrd: nobody is using them right now.
const asleep = new Set(["onboarding", "contracts", "shifts"]);

// Where each app lies while it is scattered, in rem and degrees.
const scattered = [
  [-1.5, -1.6, -8], [0.8, -2.2, 6], [2, -0.8, -4], [-2, 0.6, 5], [0.4, 0.4, -10], [1.8, 1.4, 7], [-1.4, 2, -5], [0.6, 1.8, 9], [1.6, 2.4, -3],
];

export function AppsYard({ className }: { className?: string }) {
  const [gathered, setGathered] = useState(false);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setGathered(true);
    const timer = window.setInterval(() => setGathered((g) => !g), 2200);
    return () => window.clearInterval(timer);
  }, []);

  return (
      <div
        className={cn(
          panel,
          className,
          "grid grid-rows-[auto_1fr_auto] gap-4 overflow-hidden p-5 transition-[border-color,box-shadow] duration-slow ease-move",
          gathered && "border-primary/50 shadow-[0_0_60px_-24px_var(--color-primary)]",
        )}
      >
        <p className="flex items-center gap-2 font-mono text-xs text-muted-foreground">
          {gathered ? <ShieldCheck className="size-3.5 text-primary" /> : <LockOpen className="size-3.5 text-destructive" />}
          {gathered ? "shpyrd · sign-in · roles · sleep" : "47 apps · 9 places · 0 rules"}
        </p>
        <div className="grid place-items-center py-6">
          <div className="grid grid-cols-3 gap-3">
            {apps.map((app, i) => {
              const [x, y, r] = scattered[i];
              const [icon, ink] = looks[app.name];
              const sleeping = gathered && asleep.has(app.name);
              return (
                <div
                  key={app.name}
                  className="grid w-32 gap-2 rounded-xl border border-border bg-background/90 p-3 shadow-sm transition-transform duration-slow ease-move"
                  style={{ transform: gathered ? "none" : `translate(${x}rem, ${y}rem) rotate(${r}deg)` }}
                >
                  <div className="flex items-center justify-between">
                    <Tile size="sm" variant="muted" className={ink}>
                      {icon}
                    </Tile>
                    {gathered ? <Lock className="size-3.5 text-primary" /> : <LockOpen className="size-3.5 text-destructive" />}
                  </div>
                  <div className="grid">
                    <span className="truncate text-sm font-medium">{app.name}</span>
                    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                      <span
                        className={cn(
                          "size-1.5 rounded-full",
                          !gathered ? "bg-destructive" : sleeping ? "bg-muted-foreground" : "bg-success",
                        )}
                      />
                      {app.team}
                      {sleeping && " · asleep"}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
        <p className={cn("text-center font-mono text-xs transition-colors duration-normal", gathered ? "text-primary" : "text-destructive")}>
          {gathered ? opening.after : opening.before}
        </p>
      </div>
  );
}
