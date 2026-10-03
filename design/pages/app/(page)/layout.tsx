import "@/src/styles/global.css";

// No script and no theme switch: a page that travels as one file follows
// the system's theme (scripts/pack.mjs turns what the dark class says into
// a media query). The gallery shows the page in each theme by the class.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="bg-background text-foreground">{children}</body>
    </html>
  );
}
