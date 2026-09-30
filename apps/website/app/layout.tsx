import type { Metadata } from "next";
import "@/styles/global.css";
import { Shell } from "@/components/shell";

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
      <body className="bg-background text-foreground">
        <Shell>{children}</Shell>
      </body>
    </html>
  );
}
