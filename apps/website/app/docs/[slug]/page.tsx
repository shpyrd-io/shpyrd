import { Text } from "@/components/text";
import { documents, read } from "@/lib/content";
import { shared } from "@/lib/metadata";

// The documents are known when the application is built: one page each.
export function generateStaticParams() {
  return documents().map((slug) => ({ slug }));
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const text = read((await params).slug);
  return { title: text.title, description: text.description, ...shared(`${text.title} · shpyrd`, text.description) };
}

export default async function Document({ params }: { params: Promise<{ slug: string }> }) {
  return <Text text={read((await params).slug)} />;
}
