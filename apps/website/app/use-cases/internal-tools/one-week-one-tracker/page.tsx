import { Redirect } from "@/components/continuous-section";

// "One week, one tracker" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Internal tools · One week, one tracker", robots: { index: false }, alternates: { canonical: "/use-cases/internal-tools#one-week-one-tracker" } };

export default function Page() {
  return <Redirect to="/use-cases/internal-tools#one-week-one-tracker" />;
}
