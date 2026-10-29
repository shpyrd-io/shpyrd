"use client";

// Slide 2: every team builds, nobody runs it. Each team's apps, each with a
// red mark; pointing at one says what is wrong with it. As in the original,
// the apps drop in one after another (upright in v2), while the count
// climbs with them; where they run comes next, and what it costs last.
// In the v2 (2026-10-20) it tells itself, as the home's films: once all is
// in, the apps are pointed at one after another, each saying what is wrong
// with it; pointing at one takes over.
import { useEffect, useState } from "react";
import { Coins, Hourglass, ShieldAlert, TriangleAlert } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { problem } from "@/content/tour-v3";
import { Heading, Label, Lead, panel, rise } from "./parts";

// When the apps start dropping in, and the gap between two of them, in ms.
const FIRST = 350;
const EACH = 70;
const appCount = problem.teams.reduce((n, t) => n + t.apps.length, 0);
const hostsAt = FIRST + appCount * EACH;
const costsAt = hostsAt + problem.hosts.length * EACH + 150;
// Every app with its fault, in order, for the slide to point at by itself.
const all = problem.teams
  .flatMap((t) => t.apps)
  .map((app, i) => ({ app, fault: problem.faults[i % problem.faults.length] }));
// An icon beside each cost (Giovani, 2026-10-27): risk, cost, bottleneck.
const costIcons = [
  <ShieldAlert key="r" />,
  <Coins key="c" />,
  <Hourglass key="b" />,
];
// How long each is pointed at.
const POINT = 2600;
const drop =
  "animate-in fade-in zoom-in-[1.35] blur-in-4 fill-mode-both duration-slow ease-enter";

export function Problem() {
  const [chosen, setChosen] = useState<{ app: string; fault: string } | null>(
    null,
  );
  // The app the slide points at by itself, once everything is in.
  const [auto, setAuto] = useState(-1);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    let timer = 0;
    const start = window.setTimeout(() => {
      setAuto(0);
      timer = window.setInterval(
        () => setAuto((a) => (a + 1) % all.length),
        POINT,
      );
    }, costsAt + 600);
    return () => {
      window.clearTimeout(start);
      window.clearInterval(timer);
    };
  }, []);
  const pointed = chosen ?? (auto >= 0 ? all[auto] : null);
  const setPointed = setChosen;
  // The count climbs with the apps, from nothing to the company's 47.
  const [count, setCount] = useState(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches)
      return setCount(problem.total);
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
          Every team is <span className="text-destructive">vibecoding</span>.
          <br />
          Nobody is running it.
        </Heading>
        <Lead>{problem.lead}</Lead>
      </div>

      {/* The cards 16px apart every way, as on every slide (2026-10-27). */}
      <div className="grid gap-4">
        {/* The count's card wide enough for each fault on one line (Giovani,
          2026-10-27), the apps' card giving it the room. */}
        <div className="grid gap-4 lg:grid-cols-[1fr_19.5rem]">
          <div
            className={cn(
              panel,
              "grid content-start gap-5 p-5 [animation-delay:360ms]",
              rise,
            )}
          >
            {/* What the card is, said first (Giovani, 2026-10-27): the teams'
              apps, and how to read it. */}
            <div className="grid gap-1">
              <p className="font-heading text-lg font-semibold">
                Your teams’ apps
              </p>
              <p className="text-sm text-muted-foreground">
                Each tag is an app a team built and put somewhere. Point at one
                to see what’s wrong with it.
              </p>
            </div>
            <div className="grid gap-x-8 gap-y-5 border-t border-border pt-5 sm:grid-cols-2 lg:grid-cols-4">
              {problem.teams.map((team) => (
                <div key={team.name} className="grid content-start gap-2">
                  <span className="text-xs font-medium text-muted-foreground">
                    {team.name}
                  </span>
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
                          // Upright, in v2 (Giovani, 2026-10-27): no tilt, none on the way in.
                          style={{ animationDelay: `${FIRST + at * EACH}ms` }}
                          className={cn(
                            drop,
                            // Pointed at, the tag fills red, its words white (Giovani,
                            // 2026-10-27): more than its edge turning red.
                            "relative rounded-md border border-border bg-background/70 px-2 py-1 text-xs transition-colors duration-200 hover:border-destructive hover:bg-destructive hover:text-white focus-visible:border-destructive focus-visible:bg-destructive focus-visible:text-white focus-visible:outline-none",
                            pointed?.app === app &&
                              "border-destructive bg-destructive text-white",
                          )}
                        >
                          {app}
                          {/* A small red warning triangle on each (Giovani, 2026-10-27), in
                          place of the red dot. */}
                          <TriangleAlert
                            aria-hidden="true"
                            className="absolute -top-1.5 -right-1.5 size-3.5 animate-pulse fill-destructive text-white"
                            strokeWidth={2.5}
                          />
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
              <div className="grid content-start gap-2">
                <span className="text-xs font-medium text-muted-foreground">
                  {problem.hostsLabel}
                </span>
                <div className="flex flex-wrap gap-1.5">
                  {problem.hosts.map((host, i) => (
                    <span
                      key={host}
                      style={{ animationDelay: `${hostsAt + i * EACH}ms` }}
                      className={cn(
                        drop,
                        "rounded bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground",
                      )}
                    >
                      {host}
                    </span>
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div
            className={cn(
              panel,
              "grid content-start gap-4 p-5 [animation-delay:480ms]",
              rise,
            )}
          >
            <div>
              <p className="font-heading text-5xl font-semibold text-destructive tabular-nums">
                {count}
              </p>
              <p className="text-sm text-muted-foreground">{problem.count}</p>
            </div>
            {/* Every fault in view (Giovani, 2026-10-27), the one of the app
              being pointed at lit together with it, the app named beside. */}
            <ul
              className="grid gap-1 border-t border-border pt-4 text-sm"
              aria-live="polite"
            >
              {problem.faults.map((fault) => {
                const lit = pointed?.fault === fault;
                return (
                  <li
                    key={fault}
                    className={cn(
                      "grid gap-1.5 rounded-md px-2 py-1.5 transition-colors duration-300",
                      lit
                        ? "bg-destructive/8 text-destructive"
                        : "text-muted-foreground",
                    )}
                  >
                    <span className="flex items-start gap-2 whitespace-nowrap">
                      <TriangleAlert
                        aria-hidden="true"
                        className={cn(
                          "mt-0.5 size-3.5 shrink-0 transition-colors duration-300",
                          lit ? "text-destructive" : "text-foreground/30",
                        )}
                      />
                      {fault}
                    </span>
                    {/* The app as a tag of its own, so it does not read as the
                      line going on, drawn as the app's tag beside while it is
                      pointed at: the same size, filled red, words white
                      (Giovani, 2026-10-27). */}
                    {lit && (
                      <span className="ml-5.5 w-fit rounded-md border border-destructive bg-destructive px-2 py-1 text-xs text-white animate-in fade-in-0 duration-300">
                        {pointed.app}
                      </span>
                    )}
                  </li>
                );
              })}
              {/* The room the app's tag takes under a lit fault, kept while
                none is lit (Giovani, 2026-10-27): the slide's height does not
                change when the first one lights, so nothing on it jumps. */}
              {!pointed && <li aria-hidden="true" className="h-7" />}
            </ul>
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          {problem.costs.map((c, i) => (
            <div
              key={c.title}
              style={{ animationDelay: `${costsAt + i * 120}ms` }}
              className={cn(panel, "flex items-start gap-3 p-4", rise)}
            >
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/10 text-destructive [&_svg]:size-[18px]">
                {costIcons[i]}
              </span>
              <span className="grid">
                <span className="text-sm font-medium text-destructive">
                  {c.title}
                </span>
                <span className="text-sm text-muted-foreground">{c.body}</span>
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
