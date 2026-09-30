import "../src/styles/global.css";

// No script and no theme switch: a page that travels as one file follows
// the system's theme (scripts/pack.mjs turns the dark tokens into a media
// query).
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-background text-foreground">{children}</body>
    </html>
  );
}
