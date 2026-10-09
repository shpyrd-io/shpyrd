import { Redirect } from "@/components/continuous-section";

// "Tell your agent" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Client apps · Tell your agent", robots: { index: false }, alternates: { canonical: "/use-cases/client-apps#tell-your-agent" } };

export default function Page() {
  return <Redirect to="/use-cases/client-apps#tell-your-agent" />;
}
