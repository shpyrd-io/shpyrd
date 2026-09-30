"use client";

import { useState } from "react";
import { ColorPicker } from "@shpyrd/ui/components/color-picker";
import { Field } from "@shpyrd/ui/components/field";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

// A few colours a workspace could take as its own. Data, not the palette.
const presets = ["#ff4f00", "#e11d48", "#d97706", "#16a34a", "#0284c7", "#7c3aed", "#0f172a", "#64748b", "#0d9488", "#db2777", "#4f46e5", "#171717"];

export default function Page() {
  const [color, setColor] = useState("#ff4f00");
  return (
    <>
      <Section title="With the colours the application offers, and any other">
        <Field label="Accent colour" hint="The colour of the buttons and the marks of this workspace." className="max-w-sm">
          <ColorPicker value={color} onChange={setColor} presets={presets} />
        </Field>
        <p className="text-sm text-muted-foreground">
          Chosen: <code className="font-mono">{color || "none"}</code>
        </p>
      </Section>
      <Section title="Empty, not valid, disabled">
        <Stack gap="normal" className="max-w-sm">
          <Field label="Empty: the platform's colour is used">
            <ColorPicker presets={presets.slice(0, 6)} />
          </Field>
          <Field label="Not a colour" error="Six hex digits after the #.">
            <ColorPicker defaultValue="#ff4" presets={presets.slice(0, 6)} />
          </Field>
          <Field label="Disabled">
            <ColorPicker defaultValue="#0284c7" disabled />
          </Field>
        </Stack>
      </Section>
    </>
  );
}
