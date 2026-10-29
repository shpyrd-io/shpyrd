import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { document, slugs } from "@/lib/mdx/catalog";
import { EditorialLanding } from "@/components/editorial-landing";
import { ScrollReveal } from "@/components/scroll-reveal";

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
  // The page's blocks rise in as they come into view, as on the home.
  return (
    <PageLayoutContent as="div">
      <ScrollReveal />
      <page.default components={{ EditorialLanding }} />
    </PageLayoutContent>
  );
}
