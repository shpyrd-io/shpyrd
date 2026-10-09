import { Redirect } from "@/components/continuous-section";

// "Before you approve" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For IT teams · Before you approve", robots: { index: false }, alternates: { canonical: "/for/it#before-you-approve" } };

export default function Page() {
  return <Redirect to="/for/it#before-you-approve" />;
}
