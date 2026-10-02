"use client";

import { useState } from "react";
import { tileOf, type IconChoice } from "@shpyrd/ui/components/app-icons";
import { IconPicker } from "@shpyrd/ui/components/icon-picker";
import { LauncherCard } from "@shpyrd/ui/components/launcher-card";
import { Section } from "../../section";

export default function Page() {
  const [choice, setChoice] = useState<IconChoice>({ icon: "briefcase", colour: "teal" });
  const [own, setOwn] = useState<IconChoice>({ colour: "violet", file: { src: "/samples/symbol.svg", type: "image/svg+xml" } });
  return (
    <>
      <Section title="A symbol and its colour, and the card as it will be">
        <div className="grid items-start gap-8 @3xl/page-layout:grid-cols-[minmax(0,28rem)_auto]">
          <IconPicker value={choice} onChange={setChoice} />
          <LauncherCard
            name="Corporate"
            description="Operations of the day, finance and the team."
            {...tileOf(choice)}
            colour={choice.colour}
            url="corporate.acme.shpyrd.app"
            tags={["Finance", "Operations"]}
          />
        </div>
      </Section>
      <Section title="An SVG of its own, drawn in the colour">
        <IconPicker value={own} onChange={setOwn} className="max-w-md" />
      </Section>
    </>
  );
}
