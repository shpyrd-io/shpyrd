"use client";

import { Anchor, Hexagon, Layers } from "lucide-react";
import { UseCaseCard } from "@shpyrd/ui/components/use-case-card";
import { Section } from "../../section";

const mark = (icon: React.ReactNode, name: string) => (
  <span className="flex items-center gap-2 font-heading text-xl font-semibold">
    {icon}
    {name}
  </span>
);

const stories = [
  {
    logo: mark(<Anchor className="size-6" />, "Northwind"),
    story: "Northwind's planners built a stock-count app with AI and put it online in a day, so every depot sees the same numbers.",
    name: "Maya Ortiz", role: "Head of Operations", company: "Northwind",
  },
  {
    logo: mark(<Hexagon className="size-6" />, "Globex"),
    story: "Finance at Globex moved its approvals tool out of a shared spreadsheet and onto shpyrd, behind the company sign-in. Only the people who approve can open it, and every change is a release they can roll back, which is what the auditors asked for.",
    name: "Ravi Menon", role: "Finance Lead", company: "Globex",
  },
  {
    logo: mark(<Layers className="size-6" />, "Initech"),
    story: "Support at Initech runs its triage board on shpyrd.",
    name: "Elena Fischer", role: "Support Manager", company: "Initech",
  },
];

export default function Page() {
  return (
    <>
      <Section title="One story">
        <div className="max-w-sm">
          <UseCaseCard {...stories[0]} link={{ href: "#" }} />
        </div>
      </Section>

      <Section title="In a row, stories of different lengths: heights are shared and the links line up">
        <div className="grid gap-4 @3xl:grid-cols-3">
          {stories.map((s) => (
            <UseCaseCard key={s.name} {...s} link={{ href: "#", label: "Read customer story" }} />
          ))}
        </div>
      </Section>

      <Section title="Without a link or a person">
        <div className="max-w-sm">
          <UseCaseCard logo={mark(<Hexagon className="size-6" />, "Umbrella")} story="Umbrella's data team replaced a weekly emailed report with an app everyone can open." />
        </div>
      </Section>
    </>
  );
}
