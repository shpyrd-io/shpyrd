import { Redirect } from "@/components/continuous-section";

// "Common questions" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For IT teams · Common questions", robots: { index: false }, alternates: { canonical: "/for/it#common-questions" } };

export default function Page() {
  return <Redirect to="/for/it#common-questions" />;
}
