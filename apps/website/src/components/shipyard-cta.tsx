import { Hero } from "@shpyrd/ui/components/hero";
import { Shipyard } from "@shpyrd/ui/components/shipyard";

// The end of a page, with the shipyard at work beside it: what to do next on
// the left, the yard on the right, and under the words where there is no
// room. It is the library's hero as a section heading, with the yard for its
// picture. The yard only moves while it is on the screen.
export function ShipyardCta({
  heading,
  description,
  actions,
  note,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  actions: React.ReactNode;
  note?: React.ReactNode;
}) {
  return (
    <Hero
      as="h2"
      variant="medium"
      className="border-t"
      heading={heading}
      description={description}
      actions={actions}
      note={note}
      image={<Shipyard />}
    />
  );
}
