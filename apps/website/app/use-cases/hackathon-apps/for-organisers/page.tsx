import { Redirect } from "@/components/continuous-section";

// "For organisers" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Apps from a hackathon · For organisers", robots: { index: false }, alternates: { canonical: "/use-cases/hackathon-apps#for-organisers" } };

export default function Page() {
  return <Redirect to="/use-cases/hackathon-apps#for-organisers" />;
}
