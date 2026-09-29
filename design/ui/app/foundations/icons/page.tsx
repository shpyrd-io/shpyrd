import {
  Boxes,
  Earth,
  History,
  KeyRound,
  Lock,
  Plus,
  Rocket,
  RotateCw,
  ScrollText,
  Search,
  Settings,
  Trash2,
} from "lucide-react";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

const icons = [
  ["Plus", Plus],
  ["Search", Search],
  ["Settings", Settings],
  ["Rocket", Rocket],
  ["RotateCw", RotateCw],
  ["ScrollText", ScrollText],
  ["History", History],
  ["Boxes", Boxes],
  ["Earth", Earth],
  ["Lock", Lock],
  ["KeyRound", KeyRound],
  ["Trash2", Trash2],
] as const;

export default function Page() {
  return (
    <>
      <Section title="From lucide-react">
        <div className="grid grid-cols-3 gap-4 @3xl/page-layout:grid-cols-6">
          {icons.map(([name, Icon]) => (
            <div key={name} className="grid justify-items-center gap-2 rounded-lg p-3 ring-1 ring-foreground/10">
              <Icon className="size-5" />
              <span className="font-mono text-xs text-muted-foreground">{name}</span>
            </div>
          ))}
        </div>
      </Section>
      <Section title="Sizes">
        <Stack direction="horizontal" wrap="wrap" align="end" gap="spacious">
          <Rocket className="size-3" />
          <Rocket className="size-4" />
          <Rocket className="size-5" />
          <Rocket className="size-6" />
          <Rocket className="size-8" />
        </Stack>
      </Section>
      <Section title="An icon takes the colour of the text around it">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Rocket className="size-5" />
          <Rocket className="size-5 text-muted-foreground" />
          <Rocket className="size-5 text-primary" />
          <Rocket className="size-5 text-destructive" />
        </Stack>
      </Section>
    </>
  );
}
