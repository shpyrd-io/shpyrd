import { socialImage } from "@/lib/social-image";

// The one picture of the site, for every page (src/lib/metadata.ts points
// each page's openGraph at it).
export { alt } from "@/lib/social-image";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
// Drawn once, when the site is built (a static export has no server).
export const dynamic = "force-static";

export default function Image() {
  return socialImage();
}
