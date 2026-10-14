import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { document, slugs } from "@/lib/mdx/catalog";
import { EditorialLanding } from "@/components/editorial-landing";

export const dynamicParams = false;
export function generateStaticParams() { return slugs("features").map((slug) => ({ slug })); }
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const page = await document("features", (await params).slug);
  if (!page) notFound();
  return { title: page.frontmatter.title, description: page.frontmatter.description };
}
export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const page = await document("features", (await params).slug);
  if (!page) notFound();
  return <PageLayoutContent as="div"><page.default components={{ EditorialLanding }} /></PageLayoutContent>;
}
