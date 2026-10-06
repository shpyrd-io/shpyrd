import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { join } from "node:path";
import { ImageResponse } from "next/og";
import { hero } from "@shpyrd/content/site/home";

// The picture every page of the site shows where it is shared (Open Graph,
// X): 1200 by 630, in the brand's colours, with the full logo and what the
// home page says. app/opengraph-image.tsx draws it once, when the site is
// built: there is no server to draw it on request.

// The brand: the logo's orange, and the light theme's ink on white
// (design/ui/src/styles/index.css, as hex: the renderer reads no oklch()).
const colors = {
  background: "#ffffff",
  foreground: "#0a0a0a",
  muted: "#737373",
  primary: "#ff4f00",
};

export const alt = `shpyrd - ${hero.heading}`;

// Geist, the site's font, from a package that has it as .woff: the renderer
// reads no .woff2, which is all the variable font comes as.
const fonts = createRequire(join(process.cwd(), "package.json"));
const geist = (weight: 500 | 600) =>
  readFile(fonts.resolve(`@fontsource/geist/files/geist-latin-${weight}-normal.woff`));

// The full logo, mark and name, as the brand has it (1849 by 512).
const logo = () => readFile(join(process.cwd(), "../../design/brand/logo/logo-full.png"));

export async function socialImage() {
  const [medium, semibold, png] = await Promise.all([geist(500), geist(600), logo()]);
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 72,
          background: colors.background,
          color: colors.foreground,
          fontFamily: "Geist",
          borderTop: `12px solid ${colors.primary}`,
        }}
      >
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={`data:image/png;base64,${png.toString("base64")}`} width={289} height={80} alt="" />

        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <span style={{ fontSize: 88, fontWeight: 600, lineHeight: 1.05, letterSpacing: -3 }}>{hero.heading}</span>
          <span style={{ fontSize: 32, fontWeight: 500, lineHeight: 1.35, color: colors.muted, maxWidth: 1000 }}>
            {hero.description}
          </span>
        </div>

        <span style={{ fontSize: 28, fontWeight: 600, color: colors.primary }}>shpyrd.io</span>
      </div>
    ),
    {
      width: 1200,
      height: 630,
      fonts: [
        { name: "Geist", data: medium, weight: 500, style: "normal" },
        { name: "Geist", data: semibold, weight: 600, style: "normal" },
      ],
    },
  );
}
