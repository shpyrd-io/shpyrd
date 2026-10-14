import { notFound } from "next/navigation";
import { document, slugs } from "@/lib/mdx/catalog";
import { DocumentText } from "@/components/text";
import { components } from "@/components/mdx";
import { shared } from "@/lib/metadata";

export const dynamicParams = false;
export function generateStaticParams() { return slugs("docs").map((slug) => ({ slug })); }
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const page = await document("docs", (await params).slug);
  if (!page) notFound();
  const { title, description } = page.frontmatter;
  return { title, description, ...shared(`${title} · shpyrd`, description) };
}
export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const page = await document("docs", (await params).slug);
  if (!page) notFound();
  return <DocumentText {...page.frontmatter} headings={page.headings}>
    <page.default components={components} />
  </DocumentText>;
}
