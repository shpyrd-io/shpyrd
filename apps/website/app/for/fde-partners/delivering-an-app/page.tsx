import { Redirect } from "@/components/continuous-section";

// "Delivering an app" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "For FDE partners · Delivering an app", robots: { index: false }, alternates: { canonical: "/for/fde-partners#delivering-an-app" } };

export default function Page() {
  return <Redirect to="/for/fde-partners#delivering-an-app" />;
}
