import { notFound } from "next/navigation";
import { pages, samples } from "@/src/pages";

// A page with sample words, as a person would see it.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(pages).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const page = pages[(await params).name];
  if (!page) notFound();
  return page.render(samples(page));
}
