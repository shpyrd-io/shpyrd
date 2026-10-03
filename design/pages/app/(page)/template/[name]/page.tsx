import { notFound } from "next/navigation";
import { marks, pages } from "@/src/pages";

// A page with a Go template's marks for its words: what scripts/pack.mjs
// packs for pkg/pages.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(pages).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const page = pages[(await params).name];
  if (!page) notFound();
  return page.render(marks(page));
}
