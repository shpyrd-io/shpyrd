import { Text } from "@/components/text";
import { read } from "@/lib/content";

// The first page of the site: content/docs/getting-started.mdx.
export default async function Home() {
  return <Text text={await read("docs/getting-started")} />;
}
