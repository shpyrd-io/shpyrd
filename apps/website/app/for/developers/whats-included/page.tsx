import { Redirect } from "@/components/continuous-section";

// "What's included" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For developers · What's included", robots: { index: false }, alternates: { canonical: "/for/developers#whats-included" } };

export default function Page() {
  return <Redirect to="/for/developers#whats-included" />;
}
