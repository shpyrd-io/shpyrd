import { Hero } from "@shpyrd/ui/components/hero";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { cn } from "@shpyrd/ui/lib/cn";

// The site's CTA block: the end of a page, with the shipyard at work beside
// it: what to do next on the left, the yard large on the right, and under the
// words, centred, where there is no room. It is the library's hero as a
// section heading, with the yard for its picture, in an aurora of the brand
// orange. Wherever it is used it looks the same; a page gives it its words
// and its links. The yard only moves while it is on the screen.
export function ShipyardCta({
  heading,
  description,
  actions,
  note,
  variant = "large",
  border = false,
  className,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  actions: React.ReactNode;
  note?: React.ReactNode;
  // The size of its heading.
  variant?: "medium" | "large";
  // A line over it, parting it from the section above. Off by default.
  border?: boolean;
  className?: string;
}) {
  return (
    // The call to action of the site: it stands in an aurora of the brand
    // orange (src/styles/aurora.css), glows around it reaching a little over
    // what is above and below; the page's layout cuts it at its bottom.
    // A page's grid gives it none of its own room between sections
    // (src/lib/page.ts): it keeps about 50px from what is over and under it.
    <div data-slot="cta" className="relative isolate">
      <div aria-hidden="true" className="aurora">
        <span className="light" />
        <span className="light" />
        <span className="light" />
        <span className="light" />
        <span className="rays" />
      </div>
    <Hero
      as="h2"
      variant={variant}
      // The words under the heading are the site's grey on the light page;
      // on the dark one they stand on the aurora, closer to the heading's
      // colour, so they read on its light.
      // The yard large, the words beside it narrower, and little room above
      // and under: about 50px from the section over it and from the footer.
      // The drawing starts 11.5% down its square, so the square is pulled up
      // by that much (a margin in % is of the column's width, the square's).
      className={cn(
        border && "border-t",
        "-mt-3.5 mb-6.5 [&>div]:py-0 @3xl/hero:[&>div]:grid-cols-[minmax(0,1fr)_minmax(0,1.7fr)] [&_[data-slot=hero-image]]:pointer-events-none @3xl/hero:[&_[data-slot=hero-image]]:-mt-[11.5%]",
        "dark:[&_[data-slot=hero-description]]:text-foreground/85 dark:[&_[data-slot=hero-note]]:text-foreground/75",
        className,
      )}
      heading={heading}
      description={description}
      actions={actions}
      note={note}
      image={<Shipyard />}
      align="auto"
    />
    </div>
  );
}
