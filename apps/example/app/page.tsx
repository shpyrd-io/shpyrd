import { Text } from "@/components/text";
import { read } from "@/lib/content";

// The first page of the site: apps/website/src/pages/index.md.
export default function Home() {
  return <Text text={read("index")} />;
}
