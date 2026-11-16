import { Tour } from "@/components/tour-v3/tour";

// The manifesto with no scroll (Giovani, 2026-11-11: on a 16-inch MacBook
// Pro the page scrolled and the foot went under the fold): when the tallest
// slide does not fit between the header and the foot, the slide is drawn
// smaller as a whole, its design and words as they are.
export const metadata = {
  title: "Proposal · Small Software manifesto, no scroll",
  robots: { index: false },
};

export default function Page() {
  return <Tour fit />;
}
