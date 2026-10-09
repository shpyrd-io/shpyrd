import { Redirect } from "@/components/continuous-section";

// "Your first deploy" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For developers · Your first deploy", robots: { index: false }, alternates: { canonical: "/for/developers#your-first-deploy" } };

export default function Page() {
  return <Redirect to="/for/developers#your-first-deploy" />;
}
