"use client";

import { useState } from "react";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Slider } from "@shpyrd/ui/components/slider";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [cpu, setCpu] = useState(70);
  const [bounds, setBounds] = useState<[number, number]>([2, 8]);
  return (
    <>
      <Section title="A value along the line, said by an input">
        <Field label="CPU target" hint="Percent of the size's CPU an instance may use before another is added." className="max-w-md">
          <div role="group" className="flex items-center gap-3">
            <Slider
              className="flex-1"
              min={1}
              max={100}
              value={[cpu]}
              onValueChange={([v]) => setCpu(v)}
              aria-label="CPU target slider"
            />
            <Input
              type="number"
              min={1}
              max={100}
              value={cpu}
              onChange={(e) => setCpu(Number(e.target.value) || 1)}
              suffix="%"
              className="w-24"
              aria-label="CPU target"
            />
          </div>
        </Field>
      </Section>
      <Section title="Two bounds, with the numbers at the ends">
        <Field label="Instances" hint="The count follows the load between the two." className="max-w-md">
          <div role="group" className="grid grid-cols-[5rem_1fr_5rem] items-center gap-x-3 gap-y-1">
            <Input
              type="number"
              min={1}
              max={20}
              value={bounds[0]}
              onChange={(e) => setBounds([Number(e.target.value) || 1, bounds[1]])}
              aria-label="Minimum instances"
            />
            <Slider
              min={1}
              max={20}
              value={bounds}
              onValueChange={([lo, hi]) => setBounds([lo, hi])}
              thumbLabels={["At least", "At most"]}
            />
            <Input
              type="number"
              min={1}
              max={20}
              value={bounds[1]}
              onChange={(e) => setBounds([bounds[0], Number(e.target.value) || 1])}
              aria-label="Maximum instances"
            />
            <span aria-hidden className="text-xs text-muted-foreground">
              at least
            </span>
            <span />
            <span aria-hidden className="text-right text-xs text-muted-foreground">
              at most
            </span>
          </div>
        </Field>
      </Section>
      <Section title="Steps, and disabled">
        <Stack gap="normal" className="max-w-md">
          <Slider aria-label="Quiet period" defaultValue={[30]} min={5} max={60} step={5} />
          <Slider aria-label="Quiet period" defaultValue={[30]} min={5} max={60} step={5} disabled />
        </Stack>
      </Section>
    </>
  );
}
