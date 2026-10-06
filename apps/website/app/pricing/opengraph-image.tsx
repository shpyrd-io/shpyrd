import { pricing } from "@shpyrd/content/site/pricing";
import { socialImage } from "@/lib/social-image";

export const alt = `shpyrd pricing - ${pricing.hero.heading}`;
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
// Drawn once, when the site is built (a static export has no server).
export const dynamic = "force-static";

export default function Image() {
  return socialImage({ kicker: "Pricing", title: pricing.hero.heading, description: pricing.hero.description });
}
