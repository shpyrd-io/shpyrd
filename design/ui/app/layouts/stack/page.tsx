import { Stack, StackItem } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default: down the page">
        <Stack className="max-w-sm">
          <Box />
          <Box />
          <Box />
        </Stack>
      </Section>
      <Section title="Along a line">
        <Stack direction="horizontal">
          <Box />
          <Box />
          <Box />
        </Stack>
      </Section>
      <Section title="Gaps">
        <Stack gap="spacious">
          {(["none", "tight", "condensed", "cozy", "normal", "spacious"] as const).map((gap) => (
            <Stack key={gap} direction="horizontal" align="center">
              <span className="w-24 shrink-0 font-mono text-xs text-muted-foreground">{gap}</span>
              <Stack direction="horizontal" gap={gap}>
                <Box />
                <Box />
                <Box />
              </Stack>
            </Stack>
          ))}
        </Stack>
      </Section>
      <Section title="Align: across the direction">
        <Stack direction="horizontal" align="center">
          <Box className="h-8" />
          <Box className="h-16" />
          <Box className="h-12" />
        </Stack>
        <Stack direction="horizontal" align="end">
          <Box className="h-8" />
          <Box className="h-16" />
          <Box className="h-12" />
        </Stack>
      </Section>
      <Section title="Justify: where the room that is left goes">
        <Stack direction="horizontal" justify="space-between">
          <Box />
          <Box />
          <Box />
        </Stack>
        <Stack direction="horizontal" justify="end">
          <Box />
          <Box />
        </Stack>
      </Section>
      <Section title="Wrap: what does not fit goes to the next line">
        <Stack direction="horizontal" wrap="wrap" className="max-w-xs">
          <Box />
          <Box />
          <Box />
          <Box />
          <Box />
        </Stack>
      </Section>
      <Section title="An item that takes the room that is left">
        <Stack direction="horizontal">
          <Box />
          <StackItem grow>
            <Box className="w-full" />
          </StackItem>
          <Box />
        </Stack>
      </Section>
    </>
  );
}

function Box({ className }: { className?: string }) {
  return (
    <div className={`h-10 w-20 shrink-0 rounded-lg border bg-muted/50 ${className ?? ""}`} />
  );
}
