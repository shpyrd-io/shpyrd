import { Tour } from "@/components/tour-v3/tour";

// The working copy of the Small Software manifesto (Giovani): begun from the
// v2 on 2026-10-27, and the model from then on (the v2 discarded). Since
// 2026-10-29 /small-software-manifesto shows it too; the original
// (src/components/tour) is at /proposals/manifesto-original.
export const metadata = {
  title: "Proposal · Small Software manifesto, v3",
  robots: { index: false },
};

export default function Page() {
  return <Tour />;
}
