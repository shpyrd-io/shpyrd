import * as React from "react";
import { cn } from "cn";

// A section of key figures: a line or two on why they matter, then the
// numbers themselves, large enough to be read from across a room, each with
// a caption saying what it counts. Two to four is the shape it is drawn for.
//
// A thin rule runs down the left edge of the whole block, so it reads as one
// piece however many figures it holds.
function BigNumbers({
  className,
  eyebrow,
  lead,
  statement,
  figures,
  ...props
}: Omit<React.ComponentProps<"section">, "children"> & {
  // A small label over the statement, with an orange triangle before it.
  eyebrow?: React.ReactNode;
  // The first sentence of the statement, in the page ink.
  lead?: React.ReactNode;
  // The rest of it, muted.
  statement?: React.ReactNode;
  figures: { value: React.ReactNode; caption: React.ReactNode }[];
}) {
  return (
    <section
      data-slot="big-numbers"
      className={cn("@container/numbers border-l border-foreground/15 pl-6 @2xl/numbers:pl-10", className)}
      {...props}
    >
      <div className="grid gap-12">
        <div className="grid gap-6">
          {eyebrow && (
            <p
              data-slot="big-numbers-eyebrow"
              className="flex items-center gap-2 font-mono text-xs tracking-wide text-muted-foreground uppercase"
            >
              <span aria-hidden="true" className="text-[8px] leading-none text-primary">
                ▶
              </span>
              {eyebrow}
            </p>
          )}
          {(lead || statement) && (
            <p
              data-slot="big-numbers-statement"
              className="max-w-3xl font-heading text-2xl leading-snug text-balance text-muted-foreground @2xl/numbers:text-3xl"
            >
              {lead && <span className="text-foreground">{lead}</span>}
              {lead && statement && " "}
              {statement}
            </p>
          )}
        </div>

        <dl
          data-slot="big-numbers-figures"
          className={cn(
            "m-0 grid gap-x-8 gap-y-12 @xl/numbers:grid-cols-2",
            figures.length === 3 && "@4xl/numbers:grid-cols-3",
            figures.length >= 4 && "@4xl/numbers:grid-cols-4",
          )}
        >
          {figures.map((figure, i) => (
            <div key={i} data-slot="big-numbers-figure" className="grid content-start gap-3">
              <dt
                data-slot="big-numbers-value"
                className="font-heading text-6xl leading-none font-semibold tracking-tighter text-foreground @2xl/numbers:text-7xl @5xl/numbers:text-8xl"
              >
                {figure.value}
              </dt>
              <dd
                data-slot="big-numbers-caption"
                className="m-0 max-w-[28ch] text-base text-muted-foreground"
              >
                {figure.caption}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}

export { BigNumbers };
