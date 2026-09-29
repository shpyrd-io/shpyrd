import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Wordmark">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Wordmark />
          <Wordmark className="h-10" />
        </Stack>
      </Section>
      <Section title="Mark">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <LogoMark />
          <LogoMark className="size-10" />
          <LogoMark className="size-16" />
        </Stack>
      </Section>
    </>
  );
}
