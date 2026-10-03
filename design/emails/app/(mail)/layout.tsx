import { ink } from "@/src/parts";

// The emails themselves, with no stylesheet: an email carries its styles
// on its elements. The gallery shows these pages in frames.
export default function MailLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body style={{ margin: 0, background: ink.background }}>{children}</body>
    </html>
  );
}
