"use client";

// Slide 4: the layers a request goes through, top to bottom, with the
// request's path down their side. Left alone, the next layer lights up every
// few seconds, a bar on the lit one filling until it moves on; choosing one
// stops there. The panel beside says what the lit layer does.
import { useEffect, useState } from "react";
import { Activity, Database, DoorOpen, Layers, Network, Server, User, UsersRound } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { platform } from "@shpyrd/content/site/tour";
import { Intro, Label, panel } from "./parts";

const icons: Record<string, React.ReactElement> = {
  door: <DoorOpen />,
  roles: <UsersRound />,
  network: <Network />,
  runtime: <Server />,
  data: <Database />,
};

// How long a layer stays lit before the next.
const HOLD = 4500;

export function Platform() {
  const [at, setAt] = useState(0);
  const [moving, setMoving] = useState(true);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) setMoving(false);
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(() => setAt((a) => (a + 1) % platform.layers.length), HOLD);
    return () => window.clearTimeout(timer);
  }, [moving, at]);
  const layer = platform.layers[at];

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Layers />}>{platform.label}</Label>}
        heading={
          <>
            A platform where <span className="text-primary">every app starts out safe</span>
          </>
        }
        lead={platform.lead}
      />

      <div className="grid gap-5 lg:grid-cols-2">
        <div className="relative grid gap-2">
          {/* The request's path, down through the layers, lit as far as the
              layer it has reached. */}
          <span aria-hidden="true" className="absolute top-4 bottom-12 left-[1.85rem] w-px bg-border" />
          <span
            aria-hidden="true"
            className="absolute top-4 left-[1.85rem] w-px bg-primary transition-[height] duration-slow ease-move"
            style={{ height: `calc(${((at + 1) / platform.layers.length) * 100}% - 4rem)` }}
          />
          <p className="relative flex items-center gap-3 px-3.5 text-sm text-muted-foreground">
            <span className="grid size-7 place-items-center rounded-full border border-border bg-background [&_svg]:size-3.5">
              <User />
            </span>
            {platform.who}
          </p>
          <div role="tablist" aria-orientation="vertical" aria-label={platform.label} className="grid gap-2">
            {platform.layers.map((l, i) => (
              <button
                key={l.id}
                type="button"
                role="tab"
                aria-selected={i === at}
                onClick={() => {
                  setAt(i);
                  setMoving(false);
                }}
                className={cn(
                  panel,
                  "relative flex items-center gap-3 overflow-hidden px-3 py-2.5 text-left transition-colors duration-normal ease-move",
                  i === at ? "border-primary/50 bg-primary/5" : "hover:border-foreground/20",
                )}
              >
                <span
                  className={cn(
                    "relative grid size-9 shrink-0 place-items-center rounded-lg transition-colors duration-normal [&_svg]:size-4",
                    i === at ? "bg-primary text-primary-foreground" : i < at ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground",
                  )}
                >
                  {icons[l.id]}
                </span>
                <span className="grid">
                  <span className="font-medium">{l.name}</span>
                  <span className="text-xs text-muted-foreground">{l.short}</span>
                </span>
                {/* How long until the next layer. */}
                {i === at && moving && (
                  <span aria-hidden="true" className="absolute inset-y-2 right-2 w-1 overflow-hidden rounded-full bg-primary/15">
                    <span
                      key={at}
                      className="block w-full rounded-full bg-primary motion-safe:animate-[tour-fill_linear_forwards]"
                      style={{ animationDuration: `${HOLD}ms` }}
                    />
                  </span>
                )}
              </button>
            ))}
          </div>
          <p className="relative flex items-center gap-2 rounded-xl border border-dashed border-border px-3 py-2 text-xs text-muted-foreground [&_svg]:size-3.5 [&_svg]:text-primary">
            <Activity />
            {platform.across}
          </p>
        </div>

        <div key={layer.id} role="tabpanel" className={cn(panel, "relative grid content-start gap-5 overflow-hidden p-6")}>
          <span aria-hidden="true" className="pointer-events-none absolute -right-24 -bottom-24 size-80 rounded-full bg-primary/15 blur-3xl animate-in fade-in duration-slow" />
          <p className="font-mono text-xs text-primary tabular-nums animate-in fade-in duration-normal">
            {String(at + 1).padStart(2, "0")} / {String(platform.layers.length).padStart(2, "0")}
          </p>
          <div className="grid gap-1 animate-in fade-in slide-in-from-bottom-2 duration-normal ease-enter">
            <h2 className="flex items-center gap-3 font-heading text-2xl font-semibold [&_svg]:size-6 [&_svg]:text-primary">
              {icons[layer.id]}
              {layer.name}
            </h2>
            <p className="text-muted-foreground">{layer.short}</p>
          </div>
          <ul className="grid gap-3">
            {layer.points.map((p, i) => (
              <li
                key={p}
                className="flex gap-3 animate-in fade-in slide-in-from-bottom-2 fill-mode-both duration-normal ease-enter"
                style={{ animationDelay: `${120 + i * 90}ms` }}
              >
                <span aria-hidden="true" className="mt-2 size-1.5 shrink-0 rounded-full bg-primary" />
                {p}
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}
