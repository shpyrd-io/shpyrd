"use client";

import { Testimonial, TestimonialGrid, TestimonialHighlight } from "@shpyrd/ui/components/testimonial";
import { people } from "../../people";
import { Section } from "../../section";

const cards = [
  { name: "Maya Ortiz", role: "Operations, Northwind", quote: <>The tool our team built in an afternoon was <TestimonialHighlight>live for everyone who needed it</TestimonialHighlight> the same day.</> },
  { name: "Ravi Menon", role: "Finance, Globex", quote: <>Sign-in came with it. <TestimonialHighlight>Nobody had to ask IT</TestimonialHighlight> who could open what.</> },
  { name: "Elena Fischer", role: "Support, Initech", quote: <>Every change is a numbered release, so <TestimonialHighlight>rolling back is one click</TestimonialHighlight>.</> },
  { name: "Tomas Brandt", role: "Data, Umbrella", quote: <>We stopped emailing spreadsheets. <TestimonialHighlight>The app is the source now.</TestimonialHighlight></> },
];

export default function Page() {
  return (
    <>
      <Section title="Feature, the highlight in the page ink">
        <div className="py-8">
          <Testimonial
            quote={<>You can have an app that does exactly what your team needs, and still not know how to put it online. <TestimonialHighlight>With shpyrd, it is online by the afternoon, behind a sign-in, for the people who need it.</TestimonialHighlight></>}
            name="Maya Ortiz"
            role="Head of Operations, Northwind"
          />
        </div>
      </Section>

      <Section title="Feature, the highlight in brand orange, with a picture">
        <div className="py-8">
          <Testimonial
            quote={<>We built it with AI in a day. <TestimonialHighlight tone="brand">shpyrd made it something the whole company could rely on.</TestimonialHighlight></>}
            name="Ravi Menon"
            role="Finance Lead, Globex"
            avatar={people[0].src}
          />
        </div>
      </Section>

      <Section title="Card">
        <div className="max-w-sm">
          <Testimonial variant="card" quote={cards[0].quote} name={cards[0].name} role={cards[0].role} avatar={people[1].src} />
        </div>
      </Section>

      <Section title="Cards in a grid, one, two or four across, equal in height">
        <TestimonialGrid>
          {cards.map((c) => (
            <Testimonial key={c.name} variant="card" quote={c.quote} name={c.name} role={c.role} />
          ))}
        </TestimonialGrid>
      </Section>
    </>
  );
}
