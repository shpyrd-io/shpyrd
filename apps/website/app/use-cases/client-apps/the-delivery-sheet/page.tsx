import { Redirect } from "@/components/continuous-section";

// "The delivery sheet" is now a part of the section's one page: this address takes
// whoever has it to that part.
export const metadata = { title: "Client apps · The delivery sheet", robots: { index: false }, alternates: { canonical: "/use-cases/client-apps#the-delivery-sheet" } };

export default function Page() {
  return <Redirect to="/use-cases/client-apps#the-delivery-sheet" />;
}
