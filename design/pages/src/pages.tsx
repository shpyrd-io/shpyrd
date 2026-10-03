import type { ReactNode } from "react";
import { Lock, Rocket } from "lucide-react";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { StatePage } from "@shpyrd/ui/components/state-page";

// The pages the server serves by itself, where no application answers.
// Each is drawn from its words: built, a word is a Go template's mark
// ({{.Title}}), filled by pkg/pages; in the gallery, a sample.
export type Words = { Title: string; Text: string };

export type ServerPage = {
  title: string;
  // When the server shows it.
  about: string;
  sample: Words & { Links: [string, string][] };
  render: (w: Words, links: ReactNode) => ReactNode;
};

export const pages: Record<string, ServerPage> = {
  nothing: {
    title: "Nothing here",
    about: "Nothing answers at the address: no app, no workspace, a link that expired (pkg/api/edge.go, members.go).",
    sample: { Title: "No app here", Text: "There is no app at this address.", Links: [["Dashboard", "#"]] },
    render: (w, links) => <StatePage title={w.Title} description={w.Text} action={links} />,
  },
  "no-access": {
    title: "No access",
    about: "The app is not for the person: not in a team it is for, access suspended, read-only, a suspended workspace (pkg/api/edge.go).",
    sample: {
      Title: "Available to the platform team",
      Text: "Ask a project admin to grant your team access, or sign in with an account that has it.",
      Links: [
        ["Sign in as someone else", "#"],
        ["Your apps", "#"],
      ],
    },
    render: (w, links) => <StatePage icon={<Lock />} title={w.Title} description={w.Text} action={links} />,
  },
  waking: {
    title: "Waking up",
    about: "The app is not answering yet: asleep and waking, or not ready (502 and 503 from the edge, pkg/api/edge.go).",
    sample: { Title: "The app is not answering", Text: "Try again in a moment. Its logs and status are in the dashboard.", Links: [["Dashboard", "#"]] },
    render: (w, links) => <StatePage icon={<Rocket />} waiting title={w.Title} description={w.Text} action={links} />,
  },
  "service-unavailable": {
    title: "Service unavailable",
    about: "Nothing answers right now (503): the edge, or an app's Ingress, while what should answer is starting or replaced (pkg/api/edge.go). The shipyard moves by the page's one script.",
    sample: { Title: "Service unavailable", Text: "This is not available right now. Try again in a few moments.", Links: [] },
    render: (w, links) => <StatePage picture={<Shipyard className="w-80 max-w-[80vw]" />} title={w.Title} description={w.Text} action={links} />,
  },
  mark: {
    title: "The mark",
    about: "Nothing to say: the root of the sign-in host (pkg/api/ui.go). Its words are never shown.",
    sample: { Title: "", Text: "", Links: [] },
    render: () => <StatePage />,
  },
};

export const marks: Words = { Title: "{{.Title}}", Text: "{{.Text}}" };
