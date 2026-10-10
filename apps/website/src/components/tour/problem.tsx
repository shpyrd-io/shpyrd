"use client";

// Slide 2: every team builds, nobody runs it. Each team's apps, each with a
// red mark; pointing at one says what is wrong with it. As in the original,
// the apps drop in one after another, some left crooked, while the count
// climbs with them; where they run comes next, and what it costs last.
import { useEffect, useState } from "react";
import { TriangleAlert } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { problem } from "@shpyrd/content/site/tour";
import { Heading, Label, Lead, panel } from "./parts";

// When the apps start dropping in, and the gap between two of them, in ms.
const FIRST = 350;
const EACH = 70;
const appCount = problem.teams.reduce((n, t) => n + t.apps.length, 0);
const hostsAt = FIRST + appCount * EACH;
const costsAt = hostsAt + problem.hosts.length * EACH + 150;
// The tilt an app is left with: a few are crooked, most are straight.
const tilts = [0, -3, 0, 2, 0, 0, -2, 0, 3, 0, 0, -4, 0, 1, 0];
const drop = "animate-in fade-in zoom-in-[1.35] blur-in-4 fill-mode-both duration-slow ease-enter";

export function Problem() {
  const [pointed, setPointed] = useState<{ app: string; fault: string } | null>(null);
  // The count climbs with the apps, from nothing to the company's 47.
  const [count, setCount] = useState(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setCount(problem.total);
    let frame = 0;
    const start = performance.now() + FIRST;
    const tick = (t: number) => {
      const share = Math.min(1, Math.max(0, (t - start) / (costsAt - FIRST)));
      setCount(Math.round(problem.total * (1 - (1 - share) ** 2)));
      if (share < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, []);
  let k = 0;

  return (
    <div className="grid gap-8">
      <div className="grid gap-4">
        <Label tone="destructive" icon={<TriangleAlert />}>
          {problem.label}
        </Label>
        <Heading>
          Every team is <span className="text-destructive">vibecoding</span>.<br />
          Nobody is running it.
        </Heading>
        <Lead>{problem.lead}</Lead>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_16rem]">
        <div className={cn(panel, "grid gap-x-8 gap-y-5 p-5 sm:grid-cols-2 lg:grid-cols-4")}>
          {problem.teams.map((team) => (
            <div key={team.name} className="grid content-start gap-2">
              <span className="text-[0.6875rem] tracking-[0.14em] text-muted-foreground uppercase">{team.name}</span>
              <div className="flex flex-wrap gap-2">
                {team.apps.map((app) => {
                  const at = k++;
                  const fault = problem.faults[at % problem.faults.length];
                  return (
                    <button
                      key={app}
                      type="button"
                      onMouseEnter={() => setPointed({ app, fault })}
                      onFocus={() => setPointed({ app, fault })}
                      onMouseLeave={() => setPointed(null)}
                      onBlur={() => setPointed(null)}
                      style={{ animationDelay: `${FIRST + at * EACH}ms`, rotate: `${tilts[at % tilts.length]}deg`, ["--tw-enter-rotate" as string]: `${(at % 2 ? 1 : -1) * 9}deg` }}
                      className={cn(
                        drop,
                        "relative rounded-md border border-border bg-background/70 px-2 py-1 font-mono text-xs transition-colors hover:border-destructive/60 hover:text-destructive focus-visible:border-destructive/60 focus-visible:outline-none",
                        pointed?.app === app && "border-destructive/60 text-destructive",
                      )}
                    >
                      {app}
                      <span aria-hidden="true" className="absolute -top-1 -right-1 size-2 animate-pulse rounded-full bg-destructive" />
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
          <div className="grid content-start gap-2">
            <span className="text-[0.6875rem] tracking-[0.14em] text-muted-foreground uppercase">{problem.hostsLabel}</span>
            <div className="flex flex-wrap gap-1.5">
              {problem.hosts.map((host, i) => (
                <span
                  key={host}
                  style={{ animationDelay: `${hostsAt + i * EACH}ms` }}
                  className={cn(drop, "rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground")}
                >
                  {host}
                </span>
              ))}
            </div>
          </div>
        </div>

        <div className={cn(panel, "grid content-start gap-4 p-5")}>
          <div>
            <p className="font-heading text-5xl font-semibold text-destructive tabular-nums">{count}</p>
            <p className="text-sm text-muted-foreground">{problem.count}</p>
          </div>
          <div className="min-h-16 border-t border-border pt-4 text-sm" aria-live="polite">
            {pointed ? (
              <p>
                <span className="font-mono text-destructive">{pointed.app}</span>
                <br />
                {pointed.fault}
              </p>
            ) : (
              <p className="text-muted-foreground">{problem.hover} →</p>
            )}
          </div>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        {problem.costs.map((c, i) => (
          <div
            key={c.title}
            style={{ animationDelay: `${costsAt + i * 120}ms` }}
            className={cn(panel, "p-4 animate-in fade-in slide-in-from-bottom-2 blur-in-4 fill-mode-both duration-slow ease-enter")}
          >
            <p className="text-sm font-medium text-destructive">{c.title}</p>
            <p className="text-sm text-muted-foreground">{c.body}</p>
          </div>
        ))}
      </div>
    </div>
  );
}
