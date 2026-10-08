import { Plus } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

type Item = { q: string; a: React.ReactNode; link?: { label: string; href: string } };

// Questions and their answers, as a list to open, one answer at a time in
// view: the usual way to show them. Native <details>, so it works without
// script and the answers are in the page for search and for a reader that
// reads it all. The first is open, to show that the others open too.
export function Faq({ items, className }: { items: Item[]; className?: string }) {
  return (
    <div className={cn(glass, "mx-auto w-full max-w-4xl divide-y divide-foreground/8 px-2", className)}>
      {items.map((item, i) => (
        <details key={item.q} open={i === 0} className="group/faq">
          <summary className="flex cursor-pointer list-none items-center justify-between gap-6 px-4 py-5 text-base font-semibold text-foreground transition-colors hover:text-primary [&::-webkit-details-marker]:hidden">
            {item.q}
            <Plus
              aria-hidden="true"
              className="size-5 shrink-0 text-primary transition-transform duration-200 group-open/faq:rotate-45"
            />
          </summary>
          <div className="-mt-1 grid max-w-prose gap-2 px-4 pb-5 text-sm text-muted-foreground">
            <p>{item.a}</p>
            {item.link && (
              <a href={item.link.href} className="font-medium text-foreground underline-offset-4 hover:text-primary hover:underline">
                {item.link.label} →
              </a>
            )}
          </div>
        </details>
      ))}
    </div>
  );
}
