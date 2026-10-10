"use client";

// Slide 9: for IT and security. The figures, the list of every app, and the
// audit trail, its lines arriving one after another as the platform's do.
import { useEffect, useState } from "react";
import { Ban, KeyRound, LayoutGrid, Moon, Rocket, Share2, Sun, Undo2 } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { governance } from "@shpyrd/content/site/tour";
import { Intro, Label, panel } from "./parts";

// The trail's clock: 14:02:10, then a few seconds between lines.
const at = (i: number) => {
  const s = 14 * 3600 + 2 * 60 + 10 + i * 7;
  return [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60].map((n) => String(n).padStart(2, "0")).join(":");
};
// Each event's mark, by what it says.
const kinds: [string, React.ReactElement, "neutral" | "primary" | "success" | "warning" | "info"][] = [
  ["deployed", <Rocket key="r" />, "primary"],
  ["turned away", <Ban key="b" />, "warning"],
  ["shared", <Share2 key="s" />, "info"],
  ["asleep", <Moon key="m" />, "neutral"],
  ["rolled back", <Undo2 key="u" />, "primary"],
  ["secret", <KeyRound key="k" />, "neutral"],
  ["woke", <Sun key="w" />, "success"],
];
// The trail's i-th line, for ever: the events again and again, the clock
// running on.
function line(i: number) {
  const message = governance.events[i % governance.events.length];
  const [, icon, type] = kinds.find(([word]) => message.includes(word)) ?? kinds[0];
  return { i, time: at(i), message, icon, type };
}
const SHOWN = 5;

export function Governance() {
  // How many lines have been written; the newest few are shown.
  const [written, setWritten] = useState(3);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setWritten(SHOWN);
    const timer = window.setInterval(() => setWritten((n) => n + 1), 2200);
    return () => window.clearInterval(timer);
  }, []);
  const lines = Array.from({ length: Math.min(written, SHOWN) }, (_, k) => line(written - 1 - k));

  return (
    <div className="grid gap-6">
      <Intro
        label={<Label tone="info" icon={<LayoutGrid />}>{governance.label}</Label>}
        heading={governance.heading}
        lead={governance.lead}
      />

      <div className="grid gap-3 sm:grid-cols-4">
        {governance.stats.map((s) => (
          <Stat key={s.label} label={s.label} value={s.value} className={cn(panel, "p-4")} />
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
        <div className={cn(panel, "overflow-x-auto p-2")}>
          <Table>
            <TableHeader>
              <TableRow>
                {governance.columns.map((c) => (
                  <TableHead key={c}>{c}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {governance.rows.map((r) => (
                <TableRow key={r.app}>
                  <TableCell>
                    <span className="font-mono">{r.app}</span> <span className="text-xs text-muted-foreground">{r.team}</span>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{r.owner}</TableCell>
                  <TableCell className="tabular-nums">{r.people}</TableCell>
                  <TableCell className="tabular-nums">{r.cost}</TableCell>
                  <TableCell>
                    <StatusBadge type={r.awake ? "success" : "neutral"}>{r.awake ? "awake" : "asleep"}</StatusBadge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div className={cn(panel, "grid content-start gap-2 p-4")}>
          <p className="flex items-center gap-2 text-sm">
            <span className="size-2 animate-pulse rounded-full bg-success" />
            {governance.trail}
          </p>
          {/* Newest on top: a new line opens its room, pushing the others
              down, and slides in; the oldest fade out at the foot. */}
          <Timeline clip="end" className="h-64 overflow-hidden [mask-image:linear-gradient(to_bottom,black_70%,transparent)]">
            {lines.map((l) => (
              <TimelineItem
                key={l.i}
                condensed
                icon={l.icon}
                type={l.type}
                className="animate-[tour-open_var(--transition-duration-slow)_var(--ease-move)_both] [overflow-x:visible] [overflow-y:clip]"
              >
                <span className="text-sm animate-in fade-in slide-in-from-left-2 fill-mode-both delay-150 duration-normal ease-enter">
                  <span className="mr-2 font-mono text-xs text-muted-foreground">{l.time}</span>
                  {l.message}
                </span>
              </TimelineItem>
            ))}
          </Timeline>
        </div>
      </div>
    </div>
  );
}
