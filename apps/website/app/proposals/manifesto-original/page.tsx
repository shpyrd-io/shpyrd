import { Tour } from "@/components/tour/tour";

// The original Small Software manifesto (src/components/tour), kept to
// compare, since /small-software-manifesto shows the v3 (Giovani,
// 2026-10-29).
export const metadata = {
  title: "Proposal · Small Software manifesto, the original",
  robots: { index: false },
};

export default function Page() {
  return <Tour />;
}
