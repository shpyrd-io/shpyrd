import { StatePage } from "@shpyrd/ui/components/state-page";
import { Links } from "../links";

// Nothing answers here: no app at the address, no workspace, a link that
// expired. The words are the server's (pkg/pages).
export default function Page() {
  return <StatePage title="{{.Title}}" description="{{.Text}}" action={<Links />} />;
}
