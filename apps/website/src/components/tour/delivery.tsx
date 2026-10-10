"use client";

// Slide 8: where it runs. The same four boxes either way: your sign-in and
// your systems on the left, shpyrd with your apps and their databases on the
// right, joined by a private link whose dots run along it. On shpyrd cloud
// the dashed line around them is two, your company's and ours; in your own
// cloud it opens into one around everything. The points come in one after
// another.
import { useState } from "react";
import { Building2, Cloud, Database, KeyRound, Server, Waypoints } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Tile } from "@shpyrd/ui/components/tile";
import { delivery } from "@shpyrd/content/site/tour";
import { Intro, Label, panel } from "./parts";

function Box({ icon, children, ours }: { icon: React.ReactElement; children: React.ReactNode; ours?: boolean }) {
  return (
    <div className="relative z-10 flex items-center gap-3 rounded-xl border border-border bg-background px-3 py-2.5 text-sm">
      <Tile size="sm" variant="muted" className={cn(ours && "text-primary")}>
        {icon}
      </Tile>
      {children}
    </div>
  );
}

// The private link between two boxes: dots running from left to right.
function Wire() {
  return (
    <span
      aria-hidden="true"
      className="h-px self-center bg-[radial-gradient(circle,var(--color-primary)_1px,transparent_1.5px)] bg-[length:6px_2px] bg-repeat-x opacity-70 motion-safe:animate-[tour-dots_0.8s_linear_infinite]"
    />
  );
}

export function Delivery() {
  const [where, setWhere] = useState("cloud");
  const own = where === "own";
  const option = delivery.options.find((o) => o.id === where)!;

  return (
    <div className="grid gap-8">
      <Intro label={<Label icon={<Cloud />}>{delivery.label}</Label>} heading={delivery.heading} lead={delivery.lead} />

      <Tabs value={where} onValueChange={setWhere}>
        <TabsList>
          {delivery.options.map((o) => (
            <TabsTrigger key={o.id} value={o.id} icon={o.id === "cloud" ? <Cloud /> : <Building2 />}>
              {o.name}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="grid gap-5 lg:grid-cols-[1.3fr_1fr]">
        <div className={cn(panel, "grid gap-4 p-6")}>
          <div className="relative px-5 py-10">
            {/* The dashed lines around the boxes: your company's on the left,
                ours on the right; in your own cloud, one around both. */}
            <div
              aria-hidden="true"
              className={cn(
                "absolute inset-y-0 left-0 rounded-2xl border border-dashed border-info/60 transition-[width] duration-slow ease-move",
                own ? "w-full" : "w-[calc(50%-1.25rem)]",
              )}
            />
            <div
              aria-hidden="true"
              className={cn(
                "absolute inset-y-0 right-0 w-[calc(50%-1.25rem)] rounded-2xl border border-dashed border-primary/60 bg-primary/5 transition-opacity duration-slow ease-move",
                own ? "opacity-0" : "opacity-100",
              )}
            />
            <p
              key={`left-${where}`}
              className="absolute -top-2.5 left-4 bg-card px-1.5 text-xs text-info animate-in fade-in duration-normal"
            >
              {own ? delivery.ownZone : delivery.company}
            </p>
            <p
              className={cn(
                "absolute -top-2.5 right-4 bg-card px-1.5 text-xs text-primary transition-opacity duration-normal",
                own && "opacity-0",
              )}
            >
              {delivery.options[0].name}
            </p>

            {/* The middle column is wider than the room between the two lines,
                so each box stands clear of the line around it. */}
            <div className="grid grid-cols-[1fr_5rem_1fr] gap-y-6">
              <Box icon={<KeyRound />}>{delivery.provider}</Box>
              <Wire />
              <Box icon={<Waypoints />} ours>
                {delivery.platformBox}
              </Box>
              <Box icon={<Server />}>{delivery.systems}</Box>
              <Wire />
              <Box icon={<Database />} ours>
                {delivery.databases}
              </Box>
            </div>
          </div>
          <p key={where} className="text-center font-mono text-xs text-muted-foreground animate-in fade-in duration-normal">
            {own ? delivery.ownLink : delivery.link}
          </p>
        </div>

        <ul key={where} className="grid content-start gap-2">
          {option.points.map((p, i) => (
            <li
              key={p}
              className={cn(panel, "flex gap-3 px-4 py-3 text-sm animate-in fade-in slide-in-from-right-3 fill-mode-both duration-normal ease-enter")}
              style={{ animationDelay: `${i * 90}ms` }}
            >
              <span aria-hidden="true" className="mt-1.5 size-1.5 shrink-0 rounded-full bg-primary" />
              {p}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
