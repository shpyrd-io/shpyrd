import * as React from "react";
import { cn } from "../lib/cn";
import { Hero } from "./hero";
import { Shipyard } from "./shipyard";

// The closing call to action on the brand orange (designed for the site's
// home, 2026-10-19): a band of #ff4f00 from edge to edge of the window, a
// soft white light drifting over it and white rays falling through it, the
// words white (on orange, always) and the main button black. Beside the words
// the shipyard, large, multiplied into the orange and running off the band's
// top and foot. A page gives it its words and its links; it is the last
// thing before the footer, which it meets with no room between. Still for who
// asked for less motion (src/styles/cta-band.css).
export function CtaBand({
  heading,
  description,
  actions,
  note,
  image = <Shipyard />,
  bleed = true,
  className,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  actions: React.ReactNode;
  note?: React.ReactNode;
  // Beside the words; the shipyard unless another picture is given.
  image?: React.ReactNode;
  // The band from edge to edge of the window (a page of the site); false
  // keeps it within its box, its corners rounded (a gallery, a card).
  bleed?: boolean;
  className?: string;
}) {
  return (
    <div data-slot="cta-band" className={cn("cta-band relative isolate", !bleed && "cta-band-contained", className)}>
      <div aria-hidden="true" className="cta-band-ground" />
      <div aria-hidden="true" className="cta-band-aurora">
        <span className="light" />
        <span className="light" />
        <span className="light" />
        <span className="light" />
        <span className="rays" />
      </div>
      <Hero
        as="h2"
        variant="large"
        // The words with room above and under them; the picture's column
        // twice theirs, the drawing scaled up a little and lifted, running
        // off the band; multiplied on its own box, so it takes the orange.
        className="cta-band-hero my-0 [&>div]:py-0 @3xl/hero:[&>div]:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] [&>div>div:first-child]:py-20 [&_[data-slot=hero-image]]:pointer-events-none [&_[data-slot=hero-image]]:!-my-[10%] [&_[data-slot=hero-image]]:-translate-y-[50px] [&_[data-slot=hero-image]]:scale-[1.026] [&_[data-slot=hero-image]]:mix-blend-multiply @3xl/hero:[&_[data-slot=hero-image]]:-mt-[11.5%]"
        heading={heading}
        description={description}
        actions={actions}
        note={note}
        image={image}
        align="auto"
      />
    </div>
  );
}
