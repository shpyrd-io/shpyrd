import { socialImage } from "@/lib/social-image";
import { documents, read } from "@/lib/content";

export const alt = "shpyrd documentation";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
// Drawn once, when the site is built (a static export has no server).
export const dynamic = "force-static";

// One picture per document, drawn when the site is built.
export function generateStaticParams() {
  return documents().map((slug) => ({ slug }));
}

export default async function Image({ params }: { params: Promise<{ slug: string }> }) {
  const text = read((await params).slug);
  return socialImage({ kicker: "Documentation", title: text.title, description: text.description });
}
