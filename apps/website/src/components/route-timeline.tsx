"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

type Step = { id: string; title: string; body: string };

// How long the line takes to cross, slow enough to read the steps in order.
const DRAW = 4000;

// The road from a laptop to a team, laid out across the page: a large icon
// for each step on one line, its name and a line about it under it, in the
// type of the mosaic's cards. When the reader scrolls to it, the line draws
// itself from left to right, slowly, and each step arrives as the line reaches it.
// Still, and whole, for who asked the system for less motion.
export function RouteTimeline({
  steps,
  icons,
  lit,
}: {
  steps: Step[];
  icons: Record<string, React.ReactElement>;
  // The step to light in orange: the one the page is about.
  lit?: string;
}) {
  const ref = React.useRef<HTMLOListElement>(null);
  const [shown, setShown] = React.useState(false);

  React.useEffect(() => {
    const list = ref.current;
    if (!list) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setShown(true);
    const seen = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        setShown(true);
        seen.disconnect();
      },
      { threshold: 0.35 },
    );
    seen.observe(list);
    return () => seen.disconnect();
  }, []);

  return (
    <ol
      ref={ref}
      data-shown={shown || undefined}
      className="group/route relative grid gap-10 md:grid-cols-4 md:gap-14"
    >
      {/* The line behind the icons, drawn from the first to the last. */}
      <span
        aria-hidden="true"
        className="absolute top-8 right-[12.5%] left-[12.5%] hidden h-px origin-left scale-x-0 bg-linear-to-r from-border via-primary/60 to-border transition-transform duration-[4000ms] ease-linear group-data-shown/route:scale-x-100 md:block"
      />
      {steps.map((step, i) => (
        <li
          key={step.id}
          // Each step arrives as the line reaches its icon: the line crosses
          // from the first icon to the last in DRAW ms, at an even pace.
          style={{ transitionDelay: `${Math.round((i / Math.max(steps.length - 1, 1)) * DRAW)}ms` }}
          className="relative grid justify-items-center gap-4 text-center opacity-0 transition-[opacity,translate] duration-1000 ease-out -translate-x-6 group-data-shown/route:translate-x-0 group-data-shown/route:opacity-100"
        >
          <span
            className={cn(
              "relative z-10 flex size-16 items-center justify-center rounded-2xl ring-1 [&_svg]:size-7",
              step.id === lit
                ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                : "bg-background text-primary ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
            )}
          >
            {icons[step.id]}
          </span>
          <div className="grid max-w-[24ch] gap-2">
            <h3 className="text-base font-semibold text-foreground">{step.title.replace(/\.$/, "")}</h3>
            <p className="text-sm text-muted-foreground">{step.body}</p>
          </div>
        </li>
      ))}
    </ol>
  );
}
