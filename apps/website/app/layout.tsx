import type { Metadata } from "next";
import { GoogleTagManager } from "@next/third-parties/google";
import "@/styles/global.css";
import { Shell } from "@/components/shell";
import { shared } from "@/lib/metadata";

// Google Tag Manager, when the build is given a container (NEXT_PUBLIC_GTM_ID,
// set where the site is built). The container's tags then decide what is
// measured; `shpyrd_app` tells them which part of the funnel this is.
const gtm = process.env.NEXT_PUBLIC_GTM_ID;

export const metadata: Metadata = {
  // Where the site answers: what makes the shared pictures' addresses whole.
  metadataBase: new URL("https://shpyrd.io"),
  title: { default: "shpyrd", template: "%s · shpyrd" },
  icons: { icon: "/logo.svg" },
  ...shared("shpyrd"),
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* The theme is applied before the first paint, by a file of its own. */}
        {/* eslint-disable-next-line @next/next/no-sync-scripts */}
        <script src="/theme.js" />
        {/* Which part of the funnel this is, on the data layer before the
            container's own first message: the container's tags fire at
            initialization only where shpyrd_app is set, and Next's component
            pushes its data after that message. A plain script, so it runs
            as the page is parsed. */}
        {gtm && <script dangerouslySetInnerHTML={{ __html: 'window.dataLayer=window.dataLayer||[];window.dataLayer.push({shpyrd_app:"website"});' }} />}
      </head>
      {gtm && <GoogleTagManager gtmId={gtm} />}
      <body className="bg-background text-foreground">
        <Shell>{children}</Shell>
      </body>
    </html>
  );
}
