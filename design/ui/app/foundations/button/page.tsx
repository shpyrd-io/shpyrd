import { ArrowRight, Plus, RotateCw, Settings, Trash2 } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Variants">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button>Default</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="destructive">Destructive</Button>
          <Button variant="link">Link</Button>
        </Stack>
      </Section>
      <Section title="Sizes and states">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button size="xs">Extra small</Button>
          <Button size="sm">Small</Button>
          <Button>Default</Button>
          <Button size="lg">Large</Button>
          <Button disabled>Disabled</Button>
        </Stack>
      </Section>
      <Section title="With an icon">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button icon={<Plus />}>New project</Button>
          <Button variant="outline" iconEnd={<ArrowRight />}>
            Continue
          </Button>
          <Button variant="secondary" size="sm" icon={<RotateCw />}>
            Restart
          </Button>
          <Button variant="destructive" icon={<Trash2 />}>
            Destroy
          </Button>
          <Button icon={<Plus />} disabled>
            Disabled
          </Button>
        </Stack>
      </Section>
      <Section title="Only an icon">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button variant="outline" size="icon" icon={<Settings />} aria-label="Settings" />
          <Button variant="ghost" size="icon-sm" icon={<Settings />} aria-label="Settings" />
          <Button size="icon-lg" icon={<Plus />} aria-label="New project" />
        </Stack>
      </Section>
    </>
  );
}
