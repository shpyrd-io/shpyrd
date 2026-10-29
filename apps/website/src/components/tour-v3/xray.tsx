"use client";

// Slide 3: what each app lacks, and what shpyrd gives. Since 2026-10-27
// (Giovani, from the proposal at /proposals/xray) told as a before and after
// the reader switches between, as pricing and comparison
// pages do. Above the table a two-way switch, "Today" and "On shpyrd", says
// which state is shown; beside it the score, in words and a bar. Each
// column opens with an icon and, under the pointer, says what it means and
// its technical name. Each app ends in its own score (2 / 8). A cell says
// one of three things, keyed under the table: the app has it, it is
// missing, or shpyrd gives it; on the switch the gaps fill column by
// column in the brand's orange, so what shpyrd adds stays visible.
import { useEffect, useRef, useState } from "react";
import {
  ArrowRight,
  Check,
  DatabaseBackup,
  Gauge,
  Globe,
  KeyRound,
  Moon,
  RotateCcw,
  ScanSearch,
  ScrollText,
  Boxes,
  Undo2,
  UsersRound,
  X,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Tile } from "@shpyrd/ui/components/tile";
import { LogoMark } from "@shpyrd/ui/components/brand";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@shpyrd/ui/components/tooltip";
import { xray } from "@/content/tour-v3";
import { Intro, Label, panel, mouseOnly } from "./parts";

// Each column's icon, and its technical name for who wants it.
const columnIcons = [
  <KeyRound key="0" />,
  <UsersRound key="1" />,
  <Boxes key="2" />,
  <Globe key="3" />,
  <Moon key="4" />,
  <DatabaseBackup key="5" />,
  <Undo2 key="6" />,
  <ScrollText key="7" />,
];
// Every column's name on two lines, so their icons and words line up.
const lines = [
  ["Only your", "people get in"],
  ["Each team", "sees its apps"],
  ["Kept apart", "from the rest"],
  ["A safe", "address"],
  ["Free while", "unused"],
  ["Data", "backed up"],
  ["Undo a", "bad update"],
  ["Who did", "what"],
];
const terms = [
  "Company sign-in (SSO)",
  "Roles and teams",
  "Isolation",
  "HTTPS (TLS)",
  "Sleeps when idle",
  "Backups",
  "Rollback",
  "Audit trail",
];

const total = xray.rows.length * xray.columns.length;
const before = xray.rows.reduce((n, r) => n + r.has.length, 0);

// The marks in the design system's parts (Giovani, 2026-10-27: bare marks
// read too faintly): a Tile holding the check or cross, coloured as
// StatusBadge colours its boxes, green the app has it, red it is missing;
// what shpyrd gives, filled in the brand's orange, the strongest of the
// three, so the gaps it fills stand out.
const box = {
  has: "border border-success/30 bg-success/10 text-success",
  // Missing: a light grey tile, only the cross red (Giovani, 2026-10-27).
  // No tile, only the red cross (Giovani, 2026-10-28: the grey tiles made the grid look crooked); the same room kept, so all stays in line.
  missing: "border border-transparent bg-transparent text-destructive",
  added: "border border-primary bg-primary text-primary-foreground",
};
function Cell({
  state,
  delay,
}: {
  state: "has" | "missing" | "added";
  delay: number;
}) {
  // One tile that changes in place (Giovani, 2026-10-27): on the switch its
  // colours turn and its cross gives way to a check, nothing leaving first.
  const check = state !== "missing";
  return (
    <span
      className="group/gap relative inline-grid align-middle"
      aria-label={
        state === "missing"
          ? "missing, only on shpyrd"
          : state === "added"
            ? "given by shpyrd"
            : "has it"
      }
    >
      <Tile
        size="sm"
        className={cn(
          box[state],
          "relative size-7 transition-[background-color,border-color,color] duration-normal ease-move",
        )}
        style={{ transitionDelay: `${delay}ms` }}
      >
        <X
          strokeWidth={2.5}
          className={cn(
            "absolute transition-[opacity,scale] duration-normal ease-move",
            check ? "scale-50 opacity-0" : "scale-100 opacity-100",
          )}
          style={{ transitionDelay: `${delay}ms` }}
        />
        <Check
          strokeWidth={2.5}
          className={cn(
            "absolute transition-[opacity,scale] duration-normal ease-move",
            check ? "scale-100 opacity-100" : "scale-50 opacity-0",
          )}
          style={{ transitionDelay: `${delay}ms` }}
        />
      </Tile>
      {/* A gap opens under the pointer and says where it is filled: the
          tile itself widens both ways, its shape and edge kept, over the
          cell (the table does not move), in its own grey and edge, with "Only on
          shpyrd" beside its cross (Giovani, 2026-10-27, after the tiles). */}
      {state === "missing" && (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute top-0 left-1/2 z-10 flex h-7 max-w-7 -translate-x-1/2 items-center overflow-hidden rounded-lg border border-border bg-background whitespace-nowrap opacity-0 transition-[max-width,opacity] duration-normal ease-move group-hover/gap:max-w-40 group-hover/gap:opacity-100"
        >
          <span className="grid size-[26px] shrink-0 place-items-center text-destructive [&_svg]:size-4">
            <X strokeWidth={2.5} />
          </span>
          <span className="pr-2.5 text-xs font-medium text-muted-foreground">
            Only on shpyrd
          </span>
        </span>
      )}
    </span>
  );
}

// An app's score, counting up as its gaps fill (Giovani, 2026-10-27): one
// more each time a cell in its row turns, in time with the cascade, red
// until it reaches 8 / 8, then green; back down the same way.
function Score({
  has,
  row,
  shipped,
}: {
  has: number[];
  row: number;
  shipped: boolean;
}) {
  const all = xray.columns.length;
  const [count, setCount] = useState(has.length);
  const was = useRef(shipped);
  useEffect(() => {
    // Only when the switch changes, not on the first showing.
    if (was.current === shipped) return;
    was.current = shipped;
    const gaps = xray.columns.map((_, i) => i).filter((i) => !has.includes(i));
    const timers = gaps.map((i, k) =>
      shipped
        ? window.setTimeout(
            () => setCount(has.length + k + 1),
            i * 110 + row * 40 + 200,
          )
        : window.setTimeout(() => setCount(all - k - 1), k * 60),
    );
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [shipped, has, row, all]);
  return (
    <StatusBadge
      type={count === all ? "success" : "error"}
      className="tabular-nums transition-colors duration-normal"
    >
      <span
        key={count}
        className="inline-block animate-in fade-in slide-in-from-bottom-1 duration-fast"
      >
        {count}
      </span>{" "}
      / {all}
    </StatusBadge>
  );
}

export function Xray() {
  const [shipped, setShipped] = useState(false);
  const [column, setColumn] = useState<number | null>(null);
  // The total at the card's foot counts with the cascade too (Giovani,
  // 2026-10-27): one more as each gap in the table turns, the percentage and
  // the bar with it, red until all are in place, then green.
  const [covered, setCovered] = useState(before);
  const was = useRef(shipped);
  useEffect(() => {
    if (was.current === shipped) return;
    was.current = shipped;
    const turns = xray.rows
      .flatMap((row, r) =>
        xray.columns.map((_, i) =>
          row.has.includes(i) ? -1 : i * 110 + r * 40 + 200,
        ),
      )
      .filter((t) => t >= 0)
      .sort((a, b) => a - b);
    const timers = turns.map((t, k) =>
      shipped
        ? window.setTimeout(() => setCovered(before + k + 1), t)
        : window.setTimeout(() => setCovered(total - k - 1), k * 12),
    );
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [shipped]);
  const share = Math.round((covered / total) * 100);
  const full = covered === total;

  return (
    <TooltipProvider delayDuration={100}>
      <div className="grid gap-8">
        <Intro
          label={
            <Label tone="info" icon={<ScanSearch />}>
              {xray.label}
            </Label>
          }
          heading={xray.heading}
          lead={xray.lead}
        />

        <div className={cn(panel, "-mb-2 grid min-w-0 gap-5 p-5")}>
          {/* The switch, centred in the card and larger (Giovani, 2026-10-27):
              the design system's tabs, which change what the panel shows. */}
          <div className="flex justify-center pt-3 pb-4">
            <Tabs
              value={shipped ? "shpyrd" : "today"}
              onValueChange={(v) => setShipped(v === "shpyrd")}
            >
              <TabsList aria-label="Show the apps" variant="pill">
                <TabsTrigger
                  {...mouseOnly}
                  value="today"
                  icon={
                    <span
                      className={cn(
                        "size-2! rounded-full transition-colors",
                        shipped ? "bg-muted-foreground/50" : "bg-destructive",
                      )}
                    />
                  }
                >
                  Today<span className="max-sm:hidden">, each on its own</span>
                </TabsTrigger>
                <TabsTrigger
                  {...mouseOnly}
                  value="shpyrd"
                  icon={<LogoMark className="size-5!" />}
                >
                  On shpyrd
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          {/* The matrix. On a phone only it slides sideways, its apps held
              at the left. */}
          <div className="-mx-5 overflow-x-auto px-5 max-sm:[&_tbody_th]:sticky max-sm:[&_tbody_th]:left-0 max-sm:[&_tbody_th]:z-10 max-sm:[&_tbody_th]:bg-card">
          <table
            className="w-full min-w-[56rem] border-separate border-spacing-0 text-sm"
            onMouseLeave={() => setColumn(null)}
          >
            <thead>
              <tr>
                <th
                  scope="col"
                  className="px-3 pb-3 text-left align-bottom text-xs font-normal text-muted-foreground"
                >
                  App
                </th>
                {xray.columns.map((c, i) => (
                  <th
                    key={c}
                    scope="col"
                    className={cn(
                      "rounded-t-xl px-1.5 pt-2 pb-3 align-top font-normal transition-colors",
                      column === i && "bg-muted/70",
                    )}
                    onMouseEnter={() => setColumn(i)}
                  >
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className="mx-auto grid w-24 cursor-help justify-items-center gap-2">
                          <Tile
                            variant={column === i ? "outline" : "muted"}
                            aria-hidden="true"
                            className={cn(
                              "transition-colors",
                              column === i && "text-primary",
                            )}
                          >
                            {columnIcons[i]}
                          </Tile>
                          <span className="text-xs leading-snug text-muted-foreground">
                            {lines[i][0]}
                            <br />
                            {lines[i][1]}
                          </span>
                        </span>
                      </TooltipTrigger>
                      <TooltipContent side="top">{terms[i]}</TooltipContent>
                    </Tooltip>
                  </th>
                ))}
                <th
                  scope="col"
                  className="px-1.5 pt-2 pb-3 align-top font-normal"
                >
                  <span className="mx-auto grid w-20 justify-items-center gap-2">
                    <Tile variant="muted" aria-hidden="true">
                      <Gauge />
                    </Tile>
                    <span className="text-xs leading-snug text-muted-foreground">
                      Score
                      <br />
                      of {xray.columns.length}
                    </span>
                  </span>
                </th>
              </tr>
            </thead>
            <tbody>
              {xray.rows.map((row, r) => {
                return (
                  <tr key={row.app} className="group/row">
                    <th
                      scope="row"
                      className="border-t border-border/70 px-3 py-3 text-left font-normal"
                    >
                      <span className="font-medium">{row.app}</span>{" "}
                      <span className="text-xs text-muted-foreground">
                        {row.team}
                      </span>
                    </th>
                    {xray.columns.map((c, i) => {
                      const had = row.has.includes(i);
                      // On shpyrd every cell turns orange, in one cascade, what the app had too (Giovani, 2026-10-27).
                      const state = shipped ? "added" : had ? "has" : "missing";
                      return (
                        <td
                          key={c}
                          onMouseEnter={() => setColumn(i)}
                          className={cn(
                            "border-t border-border/70 px-1.5 py-3 text-center transition-colors",
                            column === i && "bg-muted/70",
                            r === xray.rows.length - 1 &&
                              column === i &&
                              "rounded-b-xl",
                          )}
                        >
                          <Cell
                            state={state}
                            delay={shipped ? i * 110 + r * 40 : 0}
                          />
                        </td>
                      );
                    })}
                    <td className="border-t border-border/70 px-1.5 py-3 text-center">
                      <Score has={row.has} row={r} shipped={shipped} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          </div>

          {/* The key, the score centred at the card's foot (Giovani, 2026-10-27), and the step to take. */}
          <div className="grid items-center gap-4 border-t border-border pt-4 sm:grid-cols-[1fr_auto_1fr] sm:gap-6">
            <span aria-hidden="true" className="max-sm:hidden" />
            <div className="flex items-center gap-3">
              <span
                className={cn(
                  "w-[4ch] text-right font-heading text-2xl leading-none font-semibold tabular-nums transition-colors",
                  full ? "text-success" : "text-destructive",
                )}
              >
                {share}%
              </span>
              <div className="grid w-full gap-1.5 sm:w-72">
                <ProgressBar
                  value={share}
                  tone={full ? "success" : "error"}
                  size="sm"
                  className="[&_[role=progressbar]>span]:transition-[width] [&_[role=progressbar]>span]:duration-fast"
                />
                <span className="text-xs text-muted-foreground tabular-nums">
                  {covered} of {total} protections in place
                </span>
              </div>
            </div>
            <div className="flex sm:justify-end [&_button]:max-sm:w-full">
              {shipped ? (
                <Button
                  variant="outline"
                  {...mouseOnly}
                  onClick={() => setShipped(false)}
                  icon={<RotateCcw />}
                >
                  {xray.reset}
                </Button>
              ) : (
                <Button
                  {...mouseOnly}
                  onClick={() => setShipped(true)}
                  iconEnd={<ArrowRight />}
                >
                  {xray.ship}
                </Button>
              )}
            </div>
          </div>
        </div>
        {/* The key, under the card (24 px), centred and small (Giovani, 2026-10-27). */}
        <ul className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
          <li className="flex items-center gap-1.5">
            <Tile
              size="sm"
              aria-hidden="true"
              className={cn(box.has, "size-4 rounded-[5px] [&_svg]:size-2.5")}
            >
              <Check strokeWidth={2.5} />
            </Tile>
            The app has it
          </li>
          <li className="flex items-center gap-1.5">
            <Tile
              size="sm"
              aria-hidden="true"
              className={cn(
                box.missing,
                "size-4 rounded-[5px] [&_svg]:size-2.5",
              )}
            >
              <X strokeWidth={2.5} />
            </Tile>
            Missing
          </li>
          <li className="flex items-center gap-1.5">
            <Tile
              size="sm"
              aria-hidden="true"
              className={cn(box.added, "size-4 rounded-[5px] [&_svg]:size-2.5")}
            >
              <Check strokeWidth={2.5} />
            </Tile>
            Given by shpyrd, by default
          </li>
        </ul>
      </div>
    </TooltipProvider>
  );
}
