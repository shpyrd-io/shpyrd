import { Redirect } from "@/components/continuous-section";

// "Who does what" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Internal tools · Who does what", robots: { index: false }, alternates: { canonical: "/use-cases/internal-tools#who-does-what" } };

export default function Page() {
  return <Redirect to="/use-cases/internal-tools#who-does-what" />;
}
