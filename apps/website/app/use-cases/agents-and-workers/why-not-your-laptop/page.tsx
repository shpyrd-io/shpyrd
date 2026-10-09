import { Redirect } from "@/components/continuous-section";

// "Why not your laptop" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Agents and workers · Why not your laptop", robots: { index: false }, alternates: { canonical: "/use-cases/agents-and-workers#why-not-your-laptop" } };

export default function Page() {
  return <Redirect to="/use-cases/agents-and-workers#why-not-your-laptop" />;
}
