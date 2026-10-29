import { SectionIntro } from "@shpyrd/ui/components/section-intro";

// The home's features (2026-10-19): the heading centred as the home's are,
// and under it the feature blocks (each film plays only on screen, and on a
// block with a pointer, only under it: src/components/film.tsx).
export function FeatureGrid({
  heading,
  description,
  children,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="grid gap-14">
      <SectionIntro
        variant="xlarge"
        align="center"
        heading={heading}
        description={description}
        className="[&_[data-slot=section-intro-description]]:max-w-none [&_[data-slot=section-intro-heading]]:max-w-none"
      />
      {children}
    </section>
  );
}
