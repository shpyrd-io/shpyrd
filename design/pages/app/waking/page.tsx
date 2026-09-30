import { Rocket } from "lucide-react";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { Links } from "../links";

// Wait a moment: the app is waking up, or not answering yet.
export default function Page() {
  return <StatePage icon={<Rocket />} waiting title="{{.Title}}" description="{{.Text}}" action={<Links />} />;
}
