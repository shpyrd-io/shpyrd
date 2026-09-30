import { Avatar } from "@shpyrd/ui/components/avatar";
import { sizes } from "@shpyrd/ui/lib/avatar";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";
import { anonymous, people, teams } from "../../people";

export default function Page() {
  const [ana] = people;
  const [platform] = teams;
  return (
    <>
      <Section title="Sizes">
        <Stack direction="horizontal" wrap="wrap" align="end" gap="cozy">
          {sizes.map((size) => (
            <Avatar key={size} size={size} src={ana.src} alt={ana.alt} />
          ))}
        </Stack>
      </Section>
      <Section title="Square, for what is not a person">
        <Stack direction="horizontal" wrap="wrap" align="end" gap="cozy">
          {sizes.map((size) => (
            <Avatar key={size} size={size} src={platform.src} alt={platform.alt} square />
          ))}
        </Stack>
      </Section>
      <Section title="Without a picture, the initials">
        <Stack direction="horizontal" wrap="wrap" align="end" gap="cozy">
          {sizes.map((size) => (
            <Avatar key={size} size={size} alt={anonymous.alt} />
          ))}
          <Avatar size={32} alt="Billing" square />
        </Stack>
      </Section>
      <Section title="Beside a name">
        <Stack gap="condensed">
          {people.slice(0, 3).map((person) => (
            <Stack key={person.alt} direction="horizontal" align="center" gap="condensed">
              <Avatar size={24} src={person.src} alt="" />
              <span className="text-sm">{person.alt}</span>
            </Stack>
          ))}
        </Stack>
      </Section>
    </>
  );
}
