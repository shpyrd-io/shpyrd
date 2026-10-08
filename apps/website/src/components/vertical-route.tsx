"use client";

import * as React from "react";
import { cn } from "@shpyrd/ui/lib/cn";

// How long the line takes to run down the steps.
const DRAW = 4000;

type Step = {
  icon: React.ReactElement;
  // The step to light in orange: the one the page is about.
  lit?: boolean;
  children: React.ReactNode;
};

// The home page's route (route-timeline.tsx), standing up: a large icon for
// each step down one line, the words and the code beside it. When the reader
// scrolls to it, the line draws itself from the top, slowly, and each step
// arrives as the line reaches it. Still, and whole, for who asked the system
// for less motion.
export function VerticalRoute({
  steps,
  className,
  flush = false,
}: {
  steps: Step[];
  className?: string;
  // Each step's content starts level with its icon's top, not with the
  // icon's middle: for steps that are panels of their own.
  flush?: boolean;
}) {
  const ref = React.useRef<HTMLOListElement>(null);
  const [shown, setShown] = React.useState(false);
  // How far the line stops short of the list's bottom: the last step's
  // height under its icon's centre, so the line ends at the last icon.
  const [end, setEnd] = React.useState(28);

  React.useEffect(() => {
    const last = ref.current?.lastElementChild as HTMLElement | null;
    if (!last) return;
    const measure = () => setEnd(last.offsetHeight - 28);
    measure();
    const resized = new ResizeObserver(measure);
    resized.observe(last);
    return () => resized.disconnect();
  }, []);

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
      { threshold: 0.2 },
    );
    seen.observe(list);
    return () => seen.disconnect();
  }, []);

  return (
    <ol
      ref={ref}
      data-shown={shown || undefined}
      className={cn("group/route relative grid gap-10", className)}
    >
      {/* The line behind the icons, from the first to the last. */}
      <span
        aria-hidden="true"
        style={{ bottom: end }}
        className="absolute top-7 left-7 w-px origin-top -translate-x-1/2 scale-y-0 bg-linear-to-b from-border via-primary/60 to-border transition-transform duration-[4000ms] ease-linear group-data-shown/route:scale-y-100"
      />
      {steps.map((step, i) => (
        <li
          key={i}
          // Each step arrives as the line reaches its icon.
          style={{ transitionDelay: `${Math.round((i / Math.max(steps.length - 1, 1)) * DRAW)}ms` }}
          className="relative grid grid-cols-[3.5rem_minmax(0,1fr)] gap-x-6 opacity-0 transition-[opacity,translate] duration-1000 ease-out -translate-x-6 group-data-shown/route:translate-x-0 group-data-shown/route:opacity-100"
        >
          <span
            className={cn(
              "relative z-10 flex size-14 items-center justify-center rounded-2xl ring-1 [&_svg]:size-6",
              step.lit
                ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                : "bg-background text-primary ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
            )}
          >
            {step.icon}
          </span>
          <div className={cn("grid min-w-0 content-start gap-2 text-muted-foreground [&_strong]:font-semibold [&_strong]:text-foreground", !flush && "pt-3.5")}>
            {step.children}
          </div>
        </li>
      ))}
    </ol>
  );
}
