import { hero } from "@shpyrd/content/site/home";
import { socialImage } from "@/lib/social-image";

export const alt = `shpyrd - ${hero.heading}`;
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
// Drawn once, when the site is built (a static export has no server).
export const dynamic = "force-static";

export default function Image() {
  return socialImage({ title: hero.heading, description: hero.description });
}
