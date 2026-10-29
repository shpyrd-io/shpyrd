import { SleepDay } from "@/components/tour-v3/sleep-day";

// A proposal for the manifesto's slide 7 (Giovani, 2026-10-28). Slide 7
// itself is untouched.
export const metadata = {
  title: "Proposal · A day of an app",
  robots: { index: false },
};

export default function Page() {
  return (
    // On the deck's ground, without the site's chrome, as the slide would be.
    <main className="min-h-svh bg-(--page-ground) px-6 py-16">
      <div className="mx-auto w-full max-w-6xl">
        <SleepDay />
      </div>
    </main>
  );
}
