import { AvatarStack } from "@shpyrd/ui/components/avatar-stack";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";
import { anonymous, people, teams } from "../../people";

export default function Page() {
  return (
    <>
      <Section title="Two to four, spread under the pointer">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <AvatarStack avatars={people.slice(0, 2)} />
          <AvatarStack avatars={people.slice(0, 3)} />
          <AvatarStack avatars={people.slice(0, 4)} />
        </Stack>
      </Section>
      <Section title="More than the places: the last says how many">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <AvatarStack avatars={people} />
          <AvatarStack avatars={people} max={6} />
        </Stack>
      </Section>
      <Section title="Sizes">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <AvatarStack avatars={people.slice(0, 4)} size={16} />
          <AvatarStack avatars={people.slice(0, 4)} size={24} />
          <AvatarStack avatars={people.slice(0, 4)} size={32} />
          <AvatarStack avatars={people.slice(0, 4)} size={48} />
        </Stack>
      </Section>
      <Section title="Square, and mixed">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <AvatarStack avatars={teams} size={24} square />
          <AvatarStack avatars={[...people.slice(0, 2), anonymous, teams[2]]} size={24} />
        </Stack>
      </Section>
      <Section title="Aligned to the right">
        <div className="flex w-64 justify-end rounded-md border border-border p-3">
          <AvatarStack avatars={people} alignRight />
        </div>
      </Section>
      <Section title="Without the spread">
        <AvatarStack avatars={people.slice(0, 4)} size={24} expand={false} />
      </Section>
    </>
  );
}
