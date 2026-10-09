import { Redirect } from "@/components/redirect";

// The Solutions-backup pages were removed (Giovani, 2026-10-09); their
// addresses take whoever has them to the new solution page.
export const metadata = { title: "Moved", robots: { index: false }, alternates: { canonical: "/solutions/developers" } };

export default function Page() {
  return <Redirect to="/solutions/developers" />;
}
