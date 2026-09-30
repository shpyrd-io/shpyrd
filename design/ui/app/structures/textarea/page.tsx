import { Field } from "@shpyrd/ui/components/field";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default: it grows with the text">
        <Field label="Description" hint="What the project is for, in a few lines." className="max-w-sm">
          <Textarea placeholder="A small API that answers the mobile app." />
        </Field>
      </Section>
      <Section title="With text, and a fixed height">
        <Field label="Public key" className="max-w-sm">
          <Textarea
            className="h-24 resize-none font-mono text-xs"
            defaultValue="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx0c2FtcGxlS2V5Rm9yVGhlR2FsbGVyeU9ubHk ana@acme"
          />
        </Field>
      </Section>
      <Section title="States">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
          <Field label="Not valid" error="It has to say something.">
            <Textarea defaultValue=" " />
          </Field>
          <Field label="Disabled">
            <Textarea disabled placeholder="Nothing can be typed here." />
          </Field>
        </div>
      </Section>
    </>
  );
}
