// What every slide of the tour is made of: a label over the heading, the
// heading, a line under it, and panels.
import { cn } from "@shpyrd/ui/lib/cn";

export function Label({ icon, children, tone = "primary" }: { icon?: React.ReactNode; children: React.ReactNode; tone?: "primary" | "destructive" | "info" }) {
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 rounded-full border px-3 py-1 font-mono text-[0.6875rem] tracking-[0.14em] uppercase [&_svg]:size-3.5",
        tone === "primary" && "border-primary/30 bg-primary/5 text-primary",
        tone === "destructive" && "border-destructive/30 bg-destructive/5 text-destructive",
        tone === "info" && "border-info/30 bg-info/5 text-info",
      )}
    >
      {icon}
      {children}
    </span>
  );
}

export function Heading({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <h1 className={cn("font-heading text-4xl leading-[1.08] font-semibold tracking-tight text-balance sm:text-5xl", className)}>
      {children}
    </h1>
  );
}

export function Lead({ children, className }: { children: React.ReactNode; className?: string }) {
  return <p className={cn("max-w-[62ch] text-lg text-pretty text-muted-foreground", className)}>{children}</p>;
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

export const panel = "rounded-2xl border border-border bg-card";
