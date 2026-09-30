import { Badge } from "@shpyrd/ui/components/badge";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <Section title="Variants">
      <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
        <Badge>Default</Badge>
        <Badge variant="secondary">Secondary</Badge>
        <Badge variant="outline">Outline</Badge>
        <Badge variant="destructive">Destructive</Badge>
      </Stack>
    </Section>
  );
}
