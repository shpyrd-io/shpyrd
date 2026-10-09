import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { features } from "@shpyrd/content/site/features";
import { LandingPage } from "@/components/landing";

// One page for each entry of the Features menu (content/site/features.ts), all
// built the same way (landing.tsx). Known when the site is built.
// Only the addresses built here exist: any other is a 404, never a page
// rendered on request.
export const dynamicParams = false;

export function generateStaticParams() {
  return features.map((p) => ({ slug: p.slug }));
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = features.find((p) => p.slug === slug);
  return page ? { title: page.name, description: page.description } : {};
}

export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = features.find((p) => p.slug === slug);
  if (!page) notFound();
  return (
    <PageLayoutContent as="div">
      <LandingPage page={page} />
    </PageLayoutContent>
  );
}
