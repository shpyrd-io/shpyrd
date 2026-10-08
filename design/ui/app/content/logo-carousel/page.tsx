"use client";

import { LogoCarousel } from "@shpyrd/ui/components/logo-carousel";
import { Section } from "../../section";
import { fictionalLogos } from "../logos";

export default function Page() {
  return (
    <>
      <Section title="Going by slowly, in grey, until the pointer is on one">
        <LogoCarousel logos={fictionalLogos} />
      </Section>

      <Section title="With a line over it">
        <LogoCarousel label="Trusted by teams at" logos={fictionalLogos} />
      </Section>

      <Section title="Faster, one trip in 15 seconds">
        <LogoCarousel label="Trusted by teams at" speed={15} logos={fictionalLogos} />
      </Section>

      <Section title="Few logos, which still fill the row">
        <LogoCarousel logos={[...fictionalLogos.slice(0, 4), ...fictionalLogos.slice(0, 4)]} />
      </Section>
    </>
  );
}
