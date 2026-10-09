import { Redirect } from "@/components/continuous-section";

// "From demo to launch" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Client apps · From demo to launch", robots: { index: false }, alternates: { canonical: "/use-cases/client-apps#from-demo-to-launch" } };

export default function Page() {
  return <Redirect to="/use-cases/client-apps#from-demo-to-launch" />;
}
