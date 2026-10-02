import type { Metadata } from "next";
import { GoogleTagManager } from "@next/third-parties/google";
import "@/styles/global.css";
import { Shell } from "@/components/shell";

// Google Tag Manager, when the build is given a container (NEXT_PUBLIC_GTM_ID,
// set where the site is built). The container's tags then decide what is
// measured; `shpyrd_app` tells them which part of the funnel this is.
const gtm = process.env.NEXT_PUBLIC_GTM_ID;

export const metadata: Metadata = {
  title: { default: "shpyrd", template: "%s · shpyrd" },
  icons: { icon: "/logo.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* The theme is applied before the first paint, by a file of its own. */}
        {/* eslint-disable-next-line @next/next/no-sync-scripts */}
        <script src="/theme.js" />
      </head>
      {gtm && <GoogleTagManager gtmId={gtm} dataLayer={{ shpyrd_app: "website" }} />}
      <body className="bg-background text-foreground">
        <Shell>{children}</Shell>
      </body>
    </html>
  );
}
