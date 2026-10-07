import { Hero } from "@shpyrd/ui/components/hero";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { cn } from "@shpyrd/ui/lib/cn";

// The end of a page, with the shipyard at work beside it: what to do next on
// the left, the yard on the right, and under the words, centred, where there
// is no room. It is the library's hero as a section heading, with the yard for its
// picture. The yard only moves while it is on the screen.
export function ShipyardCta({
  heading,
  description,
  actions,
  note,
  variant = "medium",
  border = true,
  className,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  actions: React.ReactNode;
  note?: React.ReactNode;
  // The size of its heading; "medium" unless the page around it is louder.
  variant?: "medium" | "large";
  // The line over it, parting it from the section above. Off where that
  // section already ends with room enough.
  border?: boolean;
  className?: string;
}) {
  return (
    <Hero
      as="h2"
      variant={variant}
      className={cn(border && "border-t", className)}
      heading={heading}
      description={description}
      actions={actions}
      note={note}
      image={<Shipyard />}
      align="auto"
    />
  );
}
