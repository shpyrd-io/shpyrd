import { Redirect } from "@/components/continuous-section";

// "Rollout checklist" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Apps from a hackathon · Rollout checklist", robots: { index: false }, alternates: { canonical: "/use-cases/hackathon-apps#rollout-checklist" } };

export default function Page() {
  return <Redirect to="/use-cases/hackathon-apps#rollout-checklist" />;
}
