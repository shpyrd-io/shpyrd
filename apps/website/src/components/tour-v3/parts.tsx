// What every slide of the tour is made of: a label over the heading, the
// heading, a line under it, and panels. In the new home's look (2026-10-20):
// the label as the home's section labels, the panels its white glass cards
// with no shadow.
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

export function Label({
  icon,
  children,
  tone = "primary",
}: {
  icon?: React.ReactNode;
  children: React.ReactNode;
  tone?: "primary" | "destructive" | "info";
}) {
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 text-sm font-medium tracking-wide uppercase [&_svg]:size-4",
        tone === "primary" && "text-primary",
        tone === "destructive" && "text-destructive",
        tone === "info" && "text-info",
      )}
    >
      {icon}
      {children}
    </span>
  );
}

export function Heading({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <h1
      className={cn(
        "font-heading text-4xl leading-[1.08] font-semibold tracking-tight text-balance sm:text-5xl",
        className,
      )}
    >
      {children}
    </h1>
  );
}

export function Lead({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <p
      className={cn(
        "max-w-[62ch] text-lg text-pretty text-muted-foreground",
        className,
      )}
    >
      {children}
    </p>
  );
}

// The opening of a slide: the label, the heading and the line under it.
export function Intro({
  label,
  heading,
  lead,
}: {
  label: React.ReactNode;
  heading: React.ReactNode;
  lead: React.ReactNode;
}) {
  return (
    <div className="grid gap-4">
      {label}
      <Heading>{heading}</Heading>
      <Lead>{lead}</Lead>
    </div>
  );
}

export const panel = cn(glass, "rounded-2xl shadow-none dark:shadow-none");

// How things come in, as on the site (scroll-reveal.tsx, 2026-10-27): 16px
// low and clear, rising into place (@keyframes reveal-rise,
// src/styles/global.css); in the manifesto a little slower than on the site
// (Giovani, 2026-10-27: an interactive deck): over 1000ms, 120ms apart.
// `rise-self` tells the slide not to move the block it is in as well.
export const rise =
  "rise-self animate-[reveal-rise_1000ms_cubic-bezier(0.2,0.7,0.2,1)_both]";

// For the controls inside a slide's animation (Giovani, 2026-10-28): the
// mouse only. Left out of the keyboard's way (no tab stop) and let go of
// the focus once clicked, so the deck's keys (arrows, space) always move
// the deck and never catch in an animation's own controls.
export const mouseOnly = {
  tabIndex: -1,
  onMouseUp: (e: React.MouseEvent<HTMLElement>) => e.currentTarget.blur(),
};
