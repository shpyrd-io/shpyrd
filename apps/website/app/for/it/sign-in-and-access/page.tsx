import { Redirect } from "@/components/continuous-section";

// "Sign-in and access" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For IT teams · Sign-in and access", robots: { index: false }, alternates: { canonical: "/for/it#sign-in-and-access" } };

export default function Page() {
  return <Redirect to="/for/it#sign-in-and-access" />;
}
