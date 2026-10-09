import { Redirect } from "@/components/continuous-section";

// "Move it off your laptop" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Agents and workers · Move it off your laptop", robots: { index: false }, alternates: { canonical: "/use-cases/agents-and-workers#move-it-off-your-laptop" } };

export default function Page() {
  return <Redirect to="/use-cases/agents-and-workers#move-it-off-your-laptop" />;
}
