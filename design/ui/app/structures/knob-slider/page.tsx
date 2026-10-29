"use client";

import { useState } from "react";
import { Label } from "@shpyrd/ui/components/label";
import { KnobSlider } from "@shpyrd/ui/components/knob-slider";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [apps, setApps] = useState(47);
  const [share, setShare] = useState(30);
  return (
    <>
      <Section title="A number, on the knob">
        <Stack gap="tight" className="max-w-sm">
          <Label htmlFor="apps">How many apps your company has</Label>
          <KnobSlider id="apps" min={5} max={150} value={apps} onValueChange={setApps} />
        </Stack>
      </Section>
      <Section title="Written as it is counted">
        <Stack gap="tight" className="max-w-sm">
          <Label htmlFor="share">Traffic to the new release</Label>
          <KnobSlider id="share" min={0} max={100} step={10} value={share} onValueChange={setShare} format={(v) => `${v}%`} />
        </Stack>
      </Section>
    </>
  );
}
