import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";
import { LogoMark } from "./brand";

export type ComparisonCell = {
  // How well it does it, out of five.
  score: number;
  // A short line on why, which may hold links.
  description: React.ReactNode;
};

export type ComparisonRow = {
  label: React.ReactNode;
  // One cell for shpyrd, then one for each competitor, in the order of `competitors`.
  shpyrd: ComparisonCell;
  competitors: ComparisonCell[];
};

// Five small squares, as many lit as the score: the brand's orange for
// the best and grey for the rest. The number is written beside them, so it
// is not told by colour alone.
function Score({ score, best }: { score: number; best: boolean }) {
  return (
    <span data-slot="comparison-score" className="flex items-center gap-2 font-mono text-xs">
      <span aria-hidden className="flex gap-0.5">
        {Array.from({ length: 5 }, (_, i) => (
          <span
            key={i}
            className={cn(
              "size-2",
              i < score ? (best ? "bg-primary" : "bg-muted-foreground") : "bg-foreground/10 dark:bg-foreground/15",
            )}
          />
        ))}
      </span>
      <span className={best ? "text-primary" : "text-muted-foreground"}>{score}/5</span>
    </span>
  );
}

function Cell({ cell, best }: { cell: ComparisonCell; best: boolean }) {
  return (
    <div className="grid gap-2">
      <Score score={cell.score} best={best} />
      <p className="text-sm text-muted-foreground [&_a]:text-foreground [&_a]:underline [&_a]:underline-offset-4 [&_a:hover]:text-primary">
        {cell.description}
      </p>
    </div>
  );
}

// shpyrd set against one to three others, thing by thing, for the pages
// that say why you would choose it. One panel of glass; the row of names in
// small grey capitals; shpyrd's column lit faintly in the brand orange and
// named with the mark. A cell is a score out of five and a line on why.
// On a phone each row becomes a card: its name, then each product's answer
// under that product's name.
function ComparisonTable({
  className,
  competitors,
  corner = "What you need",
  rows,
  ...props
}: React.ComponentProps<"div"> & {
  // The names of the others: one to three.
  competitors: string[];
  // The heading of the column of names.
  corner?: string;
  rows: ComparisonRow[];
}) {
  const line = "border-foreground/8 dark:border-foreground/15";
  const head = "px-6 py-4 font-mono text-xs font-normal tracking-wider text-muted-foreground uppercase";
  const lit = "bg-primary/[0.05] dark:bg-primary/[0.08]";
  return (
    <div data-slot="comparison-table" className={cn(glass, "overflow-hidden p-0", className)} {...props}>
      <table className="w-full table-fixed border-collapse text-left max-md:block">
        <thead className="max-md:hidden">
          <tr className="bg-muted/50">
            <th scope="col" className={head}>
              {corner}
            </th>
            <th scope="col" className={cn(head, lit, "text-primary")}>
              <span className="inline-flex items-center gap-2">
                <LogoMark className="size-4" />
                shpyrd
              </span>
            </th>
            {competitors.map((name) => (
              <th key={name} scope="col" className={head}>
                {name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="max-md:grid max-md:gap-3 max-md:p-3">
          {rows.map((row, r) => (
            <tr
              key={r}
              className={cn("border-t align-top max-md:grid max-md:gap-4 max-md:rounded-xl max-md:border max-md:p-4", line)}
            >
              <th scope="row" className="px-6 py-5 font-heading text-base font-semibold text-foreground max-md:p-0">
                {row.label}
              </th>
              <td className={cn("px-6 py-5", lit, "max-md:rounded-lg max-md:p-3")}>
                <span className="mb-2 block font-mono text-[0.68rem] tracking-wider text-primary uppercase md:hidden">
                  shpyrd
                </span>
                <Cell cell={row.shpyrd} best />
              </td>
              {competitors.map((name, c) => {
                const cell = row.competitors[c];
                return (
                  <td key={name} className="px-6 py-5 max-md:p-0 max-md:px-3">
                    <span className="mb-2 block font-mono text-[0.68rem] tracking-wider text-muted-foreground uppercase md:hidden">
                      {name}
                    </span>
                    {cell && <Cell cell={cell} best={false} />}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export { ComparisonTable };
