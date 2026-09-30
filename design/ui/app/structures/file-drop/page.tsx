"use client";

import { useState } from "react";
import { Field } from "@shpyrd/ui/components/field";
import { FileDrop } from "@shpyrd/ui/components/file-drop";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

// A logo the way a workspace would keep one: an image, read into a data
// URL so it can be shown before it is sent.
const mark =
  "data:image/svg+xml;utf8," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#7c3aed"/><text x="32" y="44" font-size="34" font-family="sans-serif" font-weight="700" fill="#fff" text-anchor="middle">A</text></svg>');

export default function Page() {
  const [logo, setLogo] = useState<string | undefined>(mark);
  const [name, setName] = useState<string | undefined>("backup-2026-09.tar.gz");
  const read = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => setLogo(String(reader.result));
    reader.readAsDataURL(file);
  };
  return (
    <>
      <Section title="Empty: a drop, or a click">
        <Field label="Logo" hint="PNG, SVG, JPEG or WebP, at most 256 KB." className="max-w-md">
          <FileDrop accept="image/png,image/jpeg,image/svg+xml,image/webp" maxSize={256 * 1024} onChange={() => {}} />
        </Field>
      </Section>
      <Section title="With an image there: shown as itself, changed or removed">
        <Field label="Logo" className="max-w-md">
          <FileDrop accept="image/*" maxSize={256 * 1024} preview={logo} onChange={read} onRemove={() => setLogo(undefined)} />
        </Field>
      </Section>
      <Section title="With a file that is not an image: its name">
        <Field label="Archive" className="max-w-md">
          <FileDrop accept=".tar.gz,.zip" fileName={name} onChange={(f) => setName(f.name)} onRemove={() => setName(undefined)} />
        </Field>
      </Section>
      <Section title="Refusing what does not fit, and disabled">
        <Stack gap="normal" className="max-w-md">
          <Field label="At most 1 KB" hint="Drop anything bigger to see the refusal.">
            <FileDrop maxSize={1024} onChange={() => {}} />
          </Field>
          <Field label="Disabled">
            <FileDrop disabled onChange={() => {}} />
          </Field>
        </Stack>
      </Section>
    </>
  );
}
