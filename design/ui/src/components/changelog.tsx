import * as React from "react";
import { cn } from "cn";
import { GitCommitHorizontal } from "lucide-react";
import { Prose } from "./prose";
import { Tile } from "./tile";

// The kinds of change an entry can be, and how each is drawn: a new feature
// in the brand orange, the rest in grey.
const tags = {
  feature: { label: "New feature", style: "border-primary/40 text-primary" },
  improvement: { label: "Improvement", style: "border-foreground/20 text-muted-foreground" },
  fix: { label: "Fix", style: "border-foreground/20 text-muted-foreground" },
} as const;

// What changed, newest first, down a thin rail with a small square for each
// entry. The containing page composes its own heading and actions.
function Changelog({
  className,
  children,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div data-slot="changelog" className={cn("@container", className)} {...props}>
      <ol data-slot="changelog-list" className="relative grid gap-14 pl-12 @3xl:pl-14">
        <span
          aria-hidden
          className="absolute top-4 bottom-2 left-4 w-px -translate-x-1/2 bg-border"
        />
        {children}
      </ol>
    </div>
  );
}

// One change: on the left its name, the day it was shipped and what kind of
// change it was; on the right what it says. The left stays in view while a
// long body is read, on a wide page; on a narrow one it stacks over the body.
function ChangelogEntry({
  className,
  title,
  date,
  tag,
  children,
  ...props
}: Omit<React.ComponentProps<"li">, "title"> & {
  title: React.ReactNode;
  // The day, as it is to be read: `Oct 7, 2026`.
  date: React.ReactNode;
  tag?: keyof typeof tags;
}) {
  const kind = tag ? tags[tag] : undefined;
  return (
    <li
      data-slot="changelog-entry"
      className={cn("relative grid gap-5 @3xl:grid-cols-[minmax(0,16rem)_minmax(0,1fr)] @3xl:gap-12", className)}
      {...props}
    >
      <Tile
        aria-hidden
        size="sm"
        variant="muted"
        className="absolute top-0 -left-12 border @3xl:-left-14"
      >
        <GitCommitHorizontal />
      </Tile>
      <div data-slot="changelog-meta" className="grid content-start gap-2 @3xl:sticky @3xl:top-6 @3xl:self-start">
        <h3 className="font-heading text-xl font-semibold">{title}</h3>
        <p className="font-mono text-xs text-muted-foreground">{date}</p>
        {kind && (
          <span
            data-slot="changelog-tag"
            data-tag={tag}
            className={cn(
              "mt-1 inline-flex w-fit items-center rounded-full border px-2.5 py-0.5 text-[0.68rem] font-medium tracking-wider uppercase",
              kind.style,
            )}
          >
            {kind.label}
          </span>
        )}
      </div>
      <Prose data-slot="changelog-body" className="min-w-0">
        {children}
      </Prose>
    </li>
  );
}

// A compact archive row. The caller supplies the title link and short date.
function ChangelogSummary({ title, date, tag, children }: {
  title: React.ReactNode; date: React.ReactNode; tag?: keyof typeof tags; children: React.ReactNode;
}) {
  const kind = tag ? tags[tag] : undefined;
  return (
    <li className="grid gap-3 border-b py-6 first:pt-0 last:border-b-0 @3xl:grid-cols-[8rem_minmax(0,1fr)] @3xl:gap-5">
      <div>{kind && <span className={cn("inline-flex rounded-full border px-2.5 py-0.5 text-[0.68rem] font-medium tracking-wider uppercase", kind.style)}>{kind.label}</span>}</div>
      <div className="grid min-w-0 gap-2">
        <h3 className="font-heading text-xl font-semibold [&_a]:decoration-foreground/30 [&_a]:underline-offset-4 [&_a:hover]:underline">{title}</h3>
        <p className="text-muted-foreground">{children}</p>
        <div className="font-mono text-xs text-muted-foreground">{date}</div>
      </div>
    </li>
  );
}

export { Changelog, ChangelogEntry, ChangelogSummary };
