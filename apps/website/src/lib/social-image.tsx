import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { join } from "node:path";
import { ImageResponse } from "next/og";

// The picture a page shows where it is shared (Open Graph, X): 1200 by 630,
// in the site's dark theme, with the mark, what the page says, and the
// address. Each page's opengraph-image.tsx draws it once, when the site is
// built: there is no server to draw it on request.

// The dark theme's tokens (design/ui/src/styles/index.css), as hex: the
// renderer reads no oklch(). The orange is the mark's own.
const colors = {
  background: "#080f1c",
  foreground: "#f0f6fc",
  muted: "#a1a1a1",
  border: "#1e2636",
  primary: "#ff4f00",
};

// Geist, the site's font, from a package that has it as .woff: the renderer
// reads no .woff2, which is all the variable font comes as.
const fonts = createRequire(join(process.cwd(), "package.json"));
const geist = (weight: 500 | 600) =>
  readFile(fonts.resolve(`@fontsource/geist/files/geist-latin-${weight}-normal.woff`));

// Short titles large, long ones smaller, so most fit on one line.
const titleSize = (title: string) => (title.length <= 26 ? 80 : title.length <= 40 ? 60 : 52);

export async function socialImage({ kicker, title, description }: { kicker?: string; title: string; description?: string }) {
  const [medium, semibold, logo] = await Promise.all([
    geist(500),
    geist(600),
    readFile(join(process.cwd(), "public/logo.svg")),
  ]);
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
          borderTop: `8px solid ${colors.primary}`,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={`data:image/svg+xml;base64,${logo.toString("base64")}`} width={60} height={60} alt="" />
          <span style={{ fontSize: 44, fontWeight: 600, letterSpacing: -1 }}>shpyrd</span>
        </div>

        {/* The words take the room between the mark and the address, centred
            in it, so a longer title never runs into either. */}
        <div style={{ display: "flex", flexDirection: "column", justifyContent: "center", gap: 20, flexGrow: 1, paddingTop: 24, paddingBottom: 24 }}>
          {kicker && (
            <span style={{ fontSize: 28, fontWeight: 600, color: colors.primary, textTransform: "uppercase", letterSpacing: 2 }}>
              {kicker}
            </span>
          )}
          <span style={{ fontSize: titleSize(title), fontWeight: 600, lineHeight: 1.05, letterSpacing: -2, maxWidth: 1056 }}>
            {title}
          </span>
          {description && (
            <span style={{ fontSize: 28, fontWeight: 500, lineHeight: 1.35, color: colors.muted, maxWidth: 1000 }}>
              {description}
            </span>
          )}
        </div>

        <span style={{ fontSize: 26, fontWeight: 500, color: colors.muted }}>shpyrd.io</span>
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
