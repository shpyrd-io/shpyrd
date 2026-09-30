import { Lock } from "lucide-react";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { Links } from "../links";

// This is not for the person: no access, access suspended, read-only, a
// suspended workspace.
export default function Page() {
  return <StatePage icon={<Lock />} title="{{.Title}}" description="{{.Text}}" action={<Links />} />;
}
