import { Redirect } from "@/components/continuous-section";

// "Run it yourself" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For developers · Run it yourself", robots: { index: false }, alternates: { canonical: "/for/developers#run-it-yourself" } };

export default function Page() {
  return <Redirect to="/for/developers#run-it-yourself" />;
}
