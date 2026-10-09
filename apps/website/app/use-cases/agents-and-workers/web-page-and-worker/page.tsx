import { Redirect } from "@/components/continuous-section";

// "Web page and worker" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Agents and workers · Web page and worker", robots: { index: false }, alternates: { canonical: "/use-cases/agents-and-workers#web-page-and-worker" } };

export default function Page() {
  return <Redirect to="/use-cases/agents-and-workers#web-page-and-worker" />;
}
