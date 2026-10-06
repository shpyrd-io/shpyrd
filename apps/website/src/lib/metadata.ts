import type { Metadata } from "next";

// What a page says where it is shared. Next replaces a layout's openGraph
// with a page's rather than merging them, so every page gives the whole of
// it; the picture comes from the opengraph-image.tsx beside the page.
export function shared(title: string, description?: string): Pick<Metadata, "openGraph" | "twitter"> {
  return {
    openGraph: { title, description, siteName: "shpyrd", type: "website", locale: "en_US" },
    twitter: { card: "summary_large_image", title, description },
  };
}
