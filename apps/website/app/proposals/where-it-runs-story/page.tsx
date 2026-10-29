import { WhereItRunsStory } from "@/components/tour-v3/where-it-runs-story";

// A proposal for the manifesto's slide 8 (Giovani, 2026-10-28). Slide 8
// itself is untouched.
export const metadata = {
  title: "Proposal · Where it runs, in steps",
  robots: { index: false },
};

export default function Page() {
  return (
    // On the deck's ground, without the site's chrome, as the slide would be.
    <main className="min-h-svh bg-(--page-ground) px-6 py-16">
      <div className="mx-auto w-full max-w-6xl">
        <WhereItRunsStory />
      </div>
    </main>
  );
}
