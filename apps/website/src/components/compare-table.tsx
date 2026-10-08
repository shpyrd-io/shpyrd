import * as React from "react";
import { Check, Minus } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

// The site's informative table, in the look of its cards: one panel of glass,
// rows parted by the footer's hairline, each row's name in the cards' title
// type, its cells in their body type. As a comparison, the way things are
// today against shpyrd: today in grey with a dash; shpyrd in the page's ink
// with an orange tick, its column lit faintly in the brand orange and named
// with the mark, as the step a page is about is lit on its route. On a phone
// each row becomes a small card: its name, then the two answers.
export type CompareRow = {
  label: string;
  before: React.ReactNode;
  after: React.ReactNode;
  // Not there yet: no tick, the answer as it is.
  pending?: boolean;
};

export function CompareTable({
  before,
  beforeIcon,
  after = "On shpyrd",
  rows,
  className,
}: {
  before: string;
  beforeIcon?: React.ReactElement;
  after?: string;
  rows: CompareRow[];
  className?: string;
}) {
  const head = "text-sm font-medium";
  const line = "border-foreground/8 dark:border-foreground/15";
  return (
    <div className={cn(glass, "overflow-hidden p-0", className)}>
      <table className="w-full border-collapse text-left max-md:block">
        <thead className="max-md:hidden">
          <tr>
            <th className="w-[24%] px-6 py-5" />
            <th className={cn(head, "w-[34%] px-6 py-5 text-muted-foreground")}>
              <span className="inline-flex items-center gap-2 [&_svg]:size-4">
                {beforeIcon}
                {before}
              </span>
            </th>
            <th className={cn(head, "w-[42%] bg-primary/[0.05] px-6 py-5 text-primary dark:bg-primary/[0.08]")}>
              <span className="inline-flex items-center gap-2">
                <LogoMark className="size-4" />
                {after}
              </span>
            </th>
          </tr>
        </thead>
        <tbody className="max-md:grid max-md:gap-3 max-md:p-3">
          {rows.map((r) => (
            <tr
              key={r.label}
              className={cn("border-t align-top max-md:grid max-md:gap-3 max-md:rounded-xl max-md:border max-md:p-4", line)}
            >
              <th scope="row" className="px-6 py-5 font-heading text-base font-semibold text-foreground max-md:p-0">
                {r.label}
              </th>
              <td className="px-6 py-5 text-sm text-muted-foreground max-md:p-0">
                <span className="flex gap-2.5">
                  <Minus aria-hidden="true" className="mt-0.5 size-4 shrink-0 opacity-60" />
                  <span>
                    <span className="sr-only md:hidden">{before}: </span>
                    {r.before}
                  </span>
                </span>
              </td>
              <td className="bg-primary/[0.05] px-6 py-5 text-sm text-foreground max-md:rounded-lg max-md:p-3 dark:bg-primary/[0.08]">
                <span className="flex gap-2.5">
                  {!r.pending && <Check aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />}
                  <span>{r.after}</span>
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
