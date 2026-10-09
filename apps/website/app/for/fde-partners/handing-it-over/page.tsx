import { Redirect } from "@/components/continuous-section";

// "Handing it over" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For FDE partners · Handing it over", robots: { index: false }, alternates: { canonical: "/for/fde-partners#handing-it-over" } };

export default function Page() {
  return <Redirect to="/for/fde-partners#handing-it-over" />;
}
