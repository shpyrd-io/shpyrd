import { Checkbox } from "@shpyrd/ui/components/checkbox";
import { Label } from "@shpyrd/ui/components/label";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

const needs = ["Single sign-on", "Security or compliance review", "Support with an SLA", "Invoicing and procurement"];

export default function Page() {
  return (
    <>
      <Section title="With a label">
        <Stack direction="horizontal" align="center" gap="tight">
          <Checkbox id="terms" />
          <Label htmlFor="terms">Send me the product updates</Label>
        </Stack>
      </Section>
      <Section title="Several choices">
        <fieldset className="grid gap-3">
          <legend className="mb-1 text-sm font-medium">What do you need?</legend>
          {needs.map((need, i) => (
            <Stack key={need} direction="horizontal" align="center" gap="tight">
              <Checkbox id={`need-${i}`} defaultChecked={i === 0} />
              <Label htmlFor={`need-${i}`}>{need}</Label>
            </Stack>
          ))}
        </fieldset>
      </Section>
      <Section title="Wrong, and disabled">
        <Stack gap="normal">
          <Stack direction="horizontal" align="center" gap="tight">
            <Checkbox id="wrong" aria-invalid />
            <Label htmlFor="wrong">A choice that is required</Label>
          </Stack>
          <Stack direction="horizontal" align="center" gap="tight">
            <Checkbox id="off" disabled defaultChecked />
            <Label htmlFor="off">Already set for you</Label>
          </Stack>
        </Stack>
      </Section>
    </>
  );
}
