import { Text } from "@/components/text";
import { read } from "@/lib/content";

// Replaced by the marketing homepage in Task 5 of the plan.
export default function Home() {
  return <Text text={read("getting-started")} />;
}
