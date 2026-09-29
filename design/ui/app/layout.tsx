import type { Metadata } from "next";
import "@shpyrd/ui/styles.css";

export const metadata: Metadata = {
  title: "shpyrd design",
  icons: { icon: "/logo.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* eslint-disable-next-line @next/next/no-sync-scripts */}
        <script src="/theme.js" />
      </head>
      <body className="bg-background text-foreground">{children}</body>
    </html>
  );
}
