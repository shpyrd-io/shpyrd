import { CounterLabel } from "@shpyrd/ui/components/counter-label";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default, and primary for what asks to be seen">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <CounterLabel>2</CounterLabel>
          <CounterLabel>12</CounterLabel>
          <CounterLabel>1,204</CounterLabel>
          <CounterLabel>11K</CounterLabel>
          <CounterLabel variant="primary">3</CounterLabel>
          <CounterLabel variant="primary">99+</CounterLabel>
        </Stack>
      </Section>
      <Section title="After a name">
        <Stack gap="condensed">
          {[
            ["Processes", 2],
            ["Releases", 12],
            ["Failed", 1],
          ].map(([name, count]) => (
            <Stack key={name} direction="horizontal" align="center" gap="condensed">
              <span className="text-sm">{name}</span>
              <CounterLabel variant={name === "Failed" ? "primary" : "default"}>
                {count}
              </CounterLabel>
            </Stack>
          ))}
        </Stack>
      </Section>
    </>
  );
}
