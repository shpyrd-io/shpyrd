"use client";

// Slide 3: what each app lacks, in a table of crosses; the button ships them
// all to shpyrd and the crosses turn to checks, one column after another.
import { useState } from "react";
import { Check, RotateCcw, ScanSearch, Sparkles, X } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { xray } from "@shpyrd/content/site/tour";
import { Intro, Label, panel } from "./parts";

const cells = xray.rows.length * xray.columns.length;
const before = xray.rows.reduce((n, r) => n + r.has.length, 0);

export function Xray() {
  const [shipped, setShipped] = useState(false);
  const share = Math.round(((shipped ? cells : before) / cells) * 100);

  return (
    <div className="grid gap-8">
      <Intro label={<Label tone="info" icon={<ScanSearch />}>{xray.label}</Label>} heading={xray.heading} lead={xray.lead} />

      <div className={cn(panel, "overflow-x-auto p-5")}>
        <table className="w-full min-w-[52rem] border-separate border-spacing-y-1.5 text-sm">
          <thead>
            <tr className="text-[0.6875rem] tracking-[0.12em] text-muted-foreground uppercase">
              <th scope="col" className="px-3 pb-2 text-left font-normal">App</th>
              {xray.columns.map((c) => (
                <th key={c} scope="col" className="px-2 pb-2 text-center font-normal">{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {xray.rows.map((row) => (
              <tr key={row.app} className="bg-muted/50">
                <th scope="row" className="rounded-l-lg px-3 py-2.5 text-left font-normal">
                  <span className="font-mono">{row.app}</span> <span className="text-xs text-muted-foreground">{row.team}</span>
                </th>
                {xray.columns.map((c, i) => {
                  const ok = shipped || row.has.includes(i);
                  return (
                    <td key={c} className="px-2 py-2.5 text-center last:rounded-r-lg">
                      <span
                        aria-label={ok ? "yes" : "no"}
                        className={cn(
                          "inline-grid size-6 place-items-center rounded-md transition-colors duration-normal ease-move [&_svg]:size-3.5",
                          ok ? "bg-success/15 text-success" : "bg-destructive/10 text-destructive",
                        )}
                        style={{ transitionDelay: shipped && !row.has.includes(i) ? `${i * 90}ms` : "0ms" }}
                      >
                        {ok ? <Check /> : <X />}
                      </span>
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>

        <div className="mt-4 flex flex-wrap items-center justify-between gap-4 border-t border-border pt-4">
          <div className="flex items-center gap-3">
            <div className="h-1.5 w-48 overflow-hidden rounded-full bg-muted">
              <div
                className={cn("h-full rounded-full transition-[width] duration-slow ease-move", shipped ? "bg-success" : "bg-destructive")}
                style={{ width: `${share}%` }}
              />
            </div>
            <span className={cn("font-mono text-sm tabular-nums", shipped ? "text-success" : "text-destructive")}>
              {share}% {xray.compliance}
            </span>
          </div>
          {shipped ? (
            <Button variant="outline" onClick={() => setShipped(false)} icon={<RotateCcw />}>
              {xray.reset}
            </Button>
          ) : (
            <Button onClick={() => setShipped(true)} icon={<Sparkles />} className="rounded-full">
              {xray.ship}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
