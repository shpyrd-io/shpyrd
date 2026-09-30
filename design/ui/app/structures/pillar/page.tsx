"use client";

import { KeyRound, RefreshCw, Rocket } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const three = [
  {
    icon: <Rocket />,
    heading: "Make it available",
    description: "Deploy a compatible app from the code your team already has.",
  },
  {
    icon: <KeyRound />,
    heading: "Choose who can use it",
    description: "Share an app with Finance, Operations or another selected group.",
  },
  {
    icon: <RefreshCw />,
    heading: "Manage it over time",
    description: "Every deploy, config change and rollback is a numbered release.",
  },
];

export default function Page() {
  return (
    <>
      <Section title="Three across, which is the shape they are drawn for">
        <div className="grid gap-8 @2xl:grid-cols-3">
          {three.map((p) => (
            <Pillar key={p.heading} as="h4" heading={p.heading} description={p.description} />
          ))}
        </div>
      </Section>

      <Section title="With a cue over each, and somewhere to read more">
        <div className="grid gap-8 @2xl:grid-cols-3">
          {three.map((p) => (
            <Pillar
              key={p.heading}
              as="h4"
              icon={p.icon}
              heading={p.heading}
              description={p.description}
              link={
                <Button variant="link" size="sm" className="px-0" onClick={stay}>
                  Read more
                </Button>
              }
            />
          ))}
        </div>
      </Section>

      <Section title="Centred, which suits three or four with short words">
        <div className="grid gap-8 @2xl:grid-cols-3">
          {three.map((p) => (
            <Pillar
              key={p.heading}
              as="h4"
              align="center"
              icon={p.icon}
              heading={p.heading}
              description={p.description}
            />
          ))}
        </div>
      </Section>

      <Section title="Under the intro of a section, which is where they live">
        <div className="grid gap-8">
          <SectionIntro as="h3" heading="Three things you get" />
          <div className="grid gap-8 @2xl:grid-cols-3">
            {three.map((p) => (
              <Pillar key={p.heading} as="h4" heading={p.heading} description={p.description} />
            ))}
          </div>
        </div>
      </Section>
    </>
  );
}
