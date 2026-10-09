import { Redirect } from "@/components/continuous-section";

// "Tools people build" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Internal tools · Tools people build", robots: { index: false }, alternates: { canonical: "/use-cases/internal-tools#tools-people-build" } };

export default function Page() {
  return <Redirect to="/use-cases/internal-tools#tools-people-build" />;
}
