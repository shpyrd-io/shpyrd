import { Fragment, type ReactNode } from "react";
import { Check, Lock, Rocket } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { ClaudeIcon, CopilotIcon, CursorIcon, GeminiIcon, OpenAIIcon, VSCodeIcon, WarpIcon } from "@shpyrd/ui/components/brand-icons";
import { Button } from "@shpyrd/ui/components/button";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { StatePage } from "@shpyrd/ui/components/state-page";

// The pages the server serves by itself, where no application answers.
// Each is drawn from its words: built, a word is a Go template's mark
// ({{.Title}}) and a list is ranged over ({{range .Links}}), filled by
// pkg/pages; in the gallery, samples.
type Item = Record<string, string>;
export type Sample = Record<string, string | Item[]>;

// What a page is drawn with: its words, its lists, and what a word
// chooses among.
export type Fill = {
  word: (name: string) => string;
  each: (list: string, draw: (item: Item) => ReactNode) => ReactNode;
  choose: (name: string, options: Record<string, ReactNode>, otherwise: ReactNode) => ReactNode;
};

export type ServerPage = {
  title: string;
  // When the server shows it.
  about: string;
  sample: Sample;
  render: (fill: Fill) => ReactNode;
};

// A page's links, one button each.
function links(fill: Fill) {
  return fill.each("Links", (link) => (
    <Button asChild variant="outline" size="lg">
      <a href={link.URL}>{link.Label}</a>
    </Button>
  ));
}

export const pages: Record<string, ServerPage> = {
  nothing: {
    title: "Nothing here",
    about: "Nothing answers at the address: no app, no workspace, a link that expired (pkg/api/edge.go, members.go).",
    sample: { Title: "No app here", Text: "There is no app at this address.", Links: [{ Label: "Dashboard", URL: "#" }] },
    render: (f) => <StatePage title={f.word("Title")} description={f.word("Text")} action={links(f)} />,
  },
  "no-access": {
    title: "No access",
    about: "The app is not for the person: not in a team it is for, access suspended, read-only, a suspended workspace (pkg/api/edge.go).",
    sample: {
      Title: "Available to the platform team",
      Text: "Ask a project admin to grant your team access, or sign in with an account that has it.",
      Links: [
        { Label: "Sign in as someone else", URL: "#" },
        { Label: "Your apps", URL: "#" },
      ],
    },
    render: (f) => <StatePage icon={<Lock />} title={f.word("Title")} description={f.word("Text")} action={links(f)} />,
  },
  waking: {
    title: "Waking up",
    about: "The app is not answering: asleep and waking, or not ready (502 from the edge, pkg/api/edge.go).",
    sample: { Title: "The app is not answering", Text: "Try again in a moment. Its logs and status are in the dashboard.", Links: [{ Label: "Dashboard", URL: "#" }] },
    render: (f) => <StatePage icon={<Rocket />} waiting title={f.word("Title")} description={f.word("Text")} action={links(f)} />,
  },
  "service-unavailable": {
    title: "Service unavailable",
    about: "Nothing answers right now (503): the edge, or an app's Ingress, while what should answer is starting or replaced (pkg/api/edge.go). The shipyard moves by the page's one script.",
    sample: { Title: "Service unavailable", Text: "This is not available right now. Try again in a few moments.", Links: [] },
    render: (f) => (
      <StatePage picture={<Shipyard className="w-80 max-w-[80vw]" />} title={f.word("Title")} description={f.word("Text")} action={links(f)} />
    ),
  },
  consent: {
    title: "Connection consent",
    about: "An application (an MCP client, an agent) asks to act for the person in a workspace: allow or deny (pkg/api/oauth.go). Its policy lets no image in, and the form go to the client's redirect only.",
    sample: {
      Client: "Claude",
      // The mark of the client, when the server knows it (pkg/api/oauth.go
      // says which name is which); its initial otherwise. The name and the
      // mark are the client's say; the host of its redirect, under them, is
      // where the answer goes, and cannot be made up.
      Icon: "claude",
      Initial: "C",
      Workspace: "Acme",
      Account: "joao@acme.com",
      Host: "claude.ai",
      Scopes: [{ Text: "See your projects: their status, logs and metrics" }, { Text: "Change your projects: deploy, scale, configure" }],
      Fields: [
        { Name: "client_id", Value: "c_8f2a" },
        { Name: "csrf", Value: "x" },
      ],
    },
    render: (f) => (
      <StatePage
        picture={
          <div className="flex items-center gap-3" aria-hidden>
            <span className="inline-flex size-16 items-center justify-center rounded-2xl bg-muted font-heading text-2xl font-medium text-foreground [&_svg]:size-8">
              {f.choose(
                "Icon",
                {
                  claude: <ClaudeIcon />,
                  openai: <OpenAIIcon />,
                  gemini: <GeminiIcon />,
                  copilot: <CopilotIcon />,
                  cursor: <CursorIcon />,
                  vscode: <VSCodeIcon />,
                  warp: <WarpIcon />,
                },
                f.word("Initial"),
              )}
            </span>
            <span className="flex gap-1">
              <span className="size-1.5 rounded-full bg-muted-foreground/40" />
              <span className="size-1.5 rounded-full bg-muted-foreground/40" />
              <span className="size-1.5 rounded-full bg-muted-foreground/40" />
            </span>
            <LogoMark className="size-16" />
          </div>
        }
        title={`Allow ${f.word("Client")} to use ${f.word("Workspace")}?`}
        description={
          <>
            <strong className="font-medium text-foreground">{f.word("Host")}</strong> will act as{" "}
            <strong className="font-medium text-foreground">{f.word("Account")}</strong>, with what your roles allow.
          </>
        }
      >
        <div className="grid w-full max-w-sm gap-6 text-left">
          <div className="grid gap-2.5">
            <p className="text-sm font-medium">It will be able to</p>
            <ul className="grid gap-2 text-sm text-muted-foreground">
              {f.each("Scopes", (scope) => (
                <li className="flex items-start gap-2.5">
                  <Check className="mt-0.5 size-4 shrink-0 text-primary" />
                  <span>{scope.Text}</span>
                </li>
              ))}
            </ul>
          </div>
          <form method="post" action="/oauth/authorize" className="grid gap-2">
            {f.each("Fields", (field) => (
              <input type="hidden" name={field.Name} value={field.Value} />
            ))}
            <Button type="submit" name="decision" value="allow" size="lg" className="w-full">
              Allow
            </Button>
            <Button type="submit" name="decision" value="deny" variant="ghost" size="lg" className="w-full">
              Deny
            </Button>
          </form>
          <p className="text-center text-xs text-balance text-muted-foreground">
            You can take this back at any time on the Workspace page.
          </p>
        </div>
      </StatePage>
    ),
  },
  mark: {
    title: "The mark",
    about: "Nothing to say: the root of the sign-in host (pkg/api/ui.go). Its words are never shown.",
    sample: {},
    render: () => <StatePage />,
  },
};

// The page with a Go template's marks: a word is {{.Name}}; a list is
// ranged over, its items' words marks of their own.
export function marks(page: ServerPage): Fill {
  return {
    word: (name) => `{{.${name}}}`,
    each: (list, draw) => {
      const first = (page.sample[list] as Item[] | undefined)?.[0] ?? {};
      const item = Object.fromEntries(Object.keys(first).map((k) => [k, `{{.${k}}}`]));
      return (
        <>
          {`{{range .${list}}}`}
          {draw(item)}
          {"{{end}}"}
        </>
      );
    },
    choose: (name, options, otherwise) => (
      <>
        {Object.entries(options).map(([value, node], i) => (
          <Fragment key={value}>
            {`{{${i === 0 ? "if" : "else if"} eq .${name} "${value}"}}`}
            {node}
          </Fragment>
        ))}
        {"{{else}}"}
        {otherwise}
        {"{{end}}"}
      </>
    ),
  };
}

// The page with its sample words.
export function samples(page: ServerPage): Fill {
  return {
    word: (name) => String(page.sample[name] ?? ""),
    each: (list, draw) => {
      const items = (page.sample[list] as Item[] | undefined) ?? [];
      return items.length ? items.map((item, i) => <Fragment key={i}>{draw(item)}</Fragment>) : null;
    },
    choose: (name, options, otherwise) => options[String(page.sample[name] ?? "")] ?? otherwise,
  };
}
