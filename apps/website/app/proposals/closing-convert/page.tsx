import { ClosingConvert } from "@/components/tour-v3/closing-convert";

// A proposal for the manifesto's slide 11, read as the conversion (Giovani,
// 2026-10-29). Slide 11 itself is untouched.
export const metadata = {
  title: "Proposal · Closing, for conversion",
  robots: { index: false },
};

export default function Page() {
  return (
    // On the deck's ground, without the site's chrome, as the slide would be.
    <main className="min-h-svh bg-(--page-ground) px-6 py-16">
      <div className="mx-auto w-full max-w-6xl">
        <ClosingConvert />
      </div>
    </main>
  );
}
