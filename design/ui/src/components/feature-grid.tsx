"use client";

import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";

export type Feature = {
  icon: React.ReactElement;
  title: React.ReactNode;
  description: React.ReactNode;
};

// A section that says what you get: on the left a heading in two tones, a
// line under it and one action; on the right the features, two across, each
// with its icon, its name and a line. Stacked on a narrow page.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function FeatureGrid({
  className,
  heading,
  headingAccent,
  description,
  action,
  features,
  ...props
}: Omit<React.ComponentProps<"section">, "title"> & {
  // The first line, in the colour of the text.
  heading: React.ReactNode;
  // The second line, in the brand's orange.
  headingAccent?: React.ReactNode;
  description?: React.ReactNode;
  // One button, under the description: an outline button.
  action?: React.ReactNode;
  features: Feature[];
}) {
  return (
    <section
      data-slot="feature-grid"
      className={cn("@container grid gap-12 @3xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @3xl:gap-16", className)}
      {...props}
    >
      <div data-slot="feature-grid-intro" className="grid content-start gap-5">
        <h2 className="font-heading text-4xl leading-tight font-semibold tracking-tight">
          <span className="block">{heading}</span>
          {headingAccent && <span className="block text-primary">{headingAccent}</span>}
        </h2>
        {description && <p className="max-w-[40ch] text-muted-foreground">{description}</p>}
        {action && <div className="mt-1">{action}</div>}
      </div>
      <ul data-slot="feature-grid-list" className="grid gap-x-10 gap-y-10 @xl:grid-cols-2">
        {features.map((feature, i) => (
          <li key={i} data-slot="feature-grid-item" className="grid content-start gap-3">
            <span className="text-foreground [&_svg]:size-6 [&_svg]:shrink-0">{feature.icon}</span>
            <h3 className="font-heading text-lg font-semibold">{feature.title}</h3>
            <p className="text-sm text-muted-foreground">{feature.description}</p>
          </li>
        ))}
      </ul>
    </section>
  );
}

export type FeatureTab = {
  id: string;
  icon?: React.ReactElement;
  label: React.ReactNode;
  // What the panel shows when this one is chosen: code, a picture, anything.
  content: React.ReactNode;
  // A button at the foot of the panel, at the right.
  link?: React.ReactNode;
};

// One thing to say with several faces: a list of features, large, and a panel
// beside it that shows the one chosen. The heading is in two tones with a
// line at its right. The list is a set of tabs: arrows move through it, Home
// and End jump to its ends.
function FeatureTabs({
  className,
  heading,
  headingAccent,
  description,
  tabs,
  defaultTab = 0,
  ...props
}: Omit<React.ComponentProps<"section">, "title"> & {
  // The first line, in grey.
  heading: React.ReactNode;
  // The second line, in the colour of the text.
  headingAccent?: React.ReactNode;
  description?: React.ReactNode;
  tabs: FeatureTab[];
  // The one chosen first, by its place in `tabs`.
  defaultTab?: number;
}) {
  const id = React.useId();
  const [current, setCurrent] = React.useState(defaultTab);
  const refs = React.useRef<(HTMLButtonElement | null)[]>([]);
  const active = tabs[current];

  const move = (to: number) => {
    const next = (to + tabs.length) % tabs.length;
    setCurrent(next);
    refs.current[next]?.focus();
  };
  const onKeyDown = (event: React.KeyboardEvent) => {
    const keys: Record<string, number> = {
      ArrowDown: current + 1,
      ArrowRight: current + 1,
      ArrowUp: current - 1,
      ArrowLeft: current - 1,
      Home: 0,
      End: tabs.length - 1,
    };
    if (event.key in keys) {
      event.preventDefault();
      move(keys[event.key]);
    }
  };

  return (
    <section data-slot="feature-tabs" className={cn("@container grid gap-12", className)} {...props}>
      <div className="grid items-end gap-6 @3xl:grid-cols-2 @3xl:gap-16">
        <h2 className="font-heading text-4xl leading-tight font-semibold tracking-tight">
          <span className="block text-muted-foreground">{heading}</span>
          {headingAccent && <span className="block text-foreground">{headingAccent}</span>}
        </h2>
        {description && <p className="max-w-[48ch] text-muted-foreground">{description}</p>}
      </div>

      <div className="grid gap-8 @3xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @3xl:gap-12">
        <div
          role="tablist"
          aria-orientation="vertical"
          aria-label={typeof heading === "string" ? heading : undefined}
          data-slot="feature-tabs-list"
          onKeyDown={onKeyDown}
          className="flex gap-x-6 gap-y-3 overflow-x-auto @3xl:flex-col @3xl:overflow-visible"
        >
          {tabs.map((tab, i) => {
            const selected = i === current;
            return (
              <button
                key={tab.id}
                ref={(el) => {
                  refs.current[i] = el;
                }}
                role="tab"
                type="button"
                id={`${id}-tab-${tab.id}`}
                aria-selected={selected}
                aria-controls={`${id}-panel`}
                tabIndex={selected ? 0 : -1}
                onClick={() => setCurrent(i)}
                className={cn(
                  "flex items-center gap-3 rounded-md py-1 text-left font-heading text-2xl font-semibold tracking-tight whitespace-nowrap transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50 motion-reduce:transition-none [&_svg]:size-6 [&_svg]:shrink-0",
                  selected ? "text-foreground" : "text-muted-foreground/60 hover:text-muted-foreground",
                )}
              >
                {tab.icon}
                {tab.label}
              </button>
            );
          })}
        </div>

        <div
          role="tabpanel"
          id={`${id}-panel`}
          aria-labelledby={`${id}-tab-${active?.id}`}
          data-slot="feature-tabs-panel"
          className={cn(glass, "grid min-w-0 content-start gap-4 p-4")}
        >
          <div className="min-w-0">{active?.content}</div>
          {active?.link && <div className="flex justify-end">{active.link}</div>}
        </div>
      </div>
    </section>
  );
}

export { FeatureGrid, FeatureTabs };
