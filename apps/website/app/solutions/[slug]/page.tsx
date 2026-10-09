import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { solutions } from "@shpyrd/content/site/solutions";
import { LandingPage } from "@/components/landing";

// One page for each entry of the Solutions menu (content/site/solutions.ts), all
// built the same way (landing.tsx). Known when the site is built.
export function generateStaticParams() {
  return solutions.map((p) => ({ slug: p.slug }));
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = solutions.find((p) => p.slug === slug);
  return page ? { title: page.name, description: page.description } : {};
}

export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = solutions.find((p) => p.slug === slug);
  if (!page) notFound();
  return (
    <PageLayoutContent as="div">
      <LandingPage page={page} />
    </PageLayoutContent>
  );
}
