import { AddToAgent } from "@/components/add-to-agent";
import { SubpageStep } from "@/components/subpage-step";

// The end of an Internal tools subpage: where to read next, and the one thing
// to do. The section's own page ends with the full "connect once".
export function InternalToolsNext({ next }: { next: { title: string; href: string } }) {
  return <SubpageStep action={<AddToAgent />} next={next} />;
}
