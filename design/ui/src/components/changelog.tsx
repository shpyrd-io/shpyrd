import * as React from "react";
import { cn } from "cn";
import { Prose } from "./prose";

// The kinds of change an entry can be, and how each is drawn: a new feature
// in the brand orange, the rest in grey.
const tags = {
  feature: { label: "New feature", style: "border-primary/40 text-primary" },
  improvement: { label: "Improvement", style: "border-foreground/20 text-muted-foreground" },
  fix: { label: "Fix", style: "border-foreground/20 text-muted-foreground" },
} as const;

// What changed, newest first, down a thin rail with a small square for each
// entry. It can open with a head: the page's title, a line under it and,
// at the right, the actions that go with it (an RSS link, a copy as Markdown).
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function Changelog({
  className,
  title,
  description,
  actions,
  children,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  title?: React.ReactNode;
  description?: React.ReactNode;
  // At the right of the head: buttons or links.
  actions?: React.ReactNode;
}) {
  const hasHead = title || description || actions;
  return (
    <div data-slot="changelog" className={cn("@container grid gap-12", className)} {...props}>
      {hasHead && (
        <header data-slot="changelog-head" className="flex flex-wrap items-end justify-between gap-6">
          <div className="grid gap-2">
            {title && <h2 className="font-heading text-3xl font-semibold tracking-tight">{title}</h2>}
            {description && <p className="text-muted-foreground">{description}</p>}
          </div>
          {actions && (
            <div data-slot="changelog-actions" className="flex flex-wrap items-center gap-2">
              {actions}
            </div>
          )}
        </header>
      )}
      <ol data-slot="changelog-list" className="relative grid gap-14 pl-6 @3xl:pl-8">
        <span
          aria-hidden
          className="absolute top-2 bottom-2 left-[3px] w-px bg-foreground/8 dark:bg-foreground/15"
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
      <span
        aria-hidden
        className="absolute top-2 -left-6 size-[7px] bg-foreground @3xl:-left-8"
      />
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

export { Changelog, ChangelogEntry };
