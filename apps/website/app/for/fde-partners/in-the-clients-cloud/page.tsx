import { Redirect } from "@/components/continuous-section";

// "In the client's cloud" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For FDE partners · In the client's cloud", robots: { index: false }, alternates: { canonical: "/for/fde-partners#in-the-clients-cloud" } };

export default function Page() {
  return <Redirect to="/for/fde-partners#in-the-clients-cloud" />;
}
