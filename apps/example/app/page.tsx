import { Text } from "@/components/text";
import { read } from "@/lib/content";

// The first page of the site: content/docs/getting-started.md.
export default function Home() {
  return <Text text={read("docs/getting-started")} />;
}
