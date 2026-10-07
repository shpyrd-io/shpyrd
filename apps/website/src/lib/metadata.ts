import type { Metadata } from "next";
import { alt } from "@/lib/social-image";

// What a page says where it is shared. Next replaces a layout's openGraph
// with a page's rather than merging them, so every page gives the whole of
// it, the picture included: the site's one, app/opengraph-image.tsx.
const image = { url: "/opengraph-image", width: 1200, height: 630, type: "image/png", alt };

export function shared(title: string, description?: string): Pick<Metadata, "openGraph" | "twitter"> {
  return {
    openGraph: { title, description, siteName: "shpyrd", type: "website", locale: "en_US", images: [image] },
    twitter: { card: "summary_large_image", title, description, images: [image] },
  };
}
