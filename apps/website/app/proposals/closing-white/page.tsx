import { ClosingStory } from "@/components/tour-v3/closing-story";

// A proposal for the manifesto's slide 11, the white version (Giovani,
// 2026-10-29). Slide 11 itself is untouched.
export const metadata = {
  title: "Proposal · Closing, white",
  robots: { index: false },
};

export default function Page() {
  return (
    // On the deck's ground, without the site's chrome, as the slide would be.
    <main className="min-h-svh bg-(--page-ground) px-6 py-16">
      <div className="mx-auto w-full max-w-6xl">
        <ClosingStory tone="white" />
      </div>
    </main>
  );
}
