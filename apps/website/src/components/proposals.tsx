import Link from "next/link";
import { ClipboardList, FileText, Gauge, Receipt, UserPlus } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { developerCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { ShipyardCta } from "@/components/shipyard-cta";

// What the four homepage proposals share while they are being compared. None of
// this outlives the decision: the one that wins moves to `/`, and its screens to
// design/ui if they earn it.

export const proposals = [
  { n: 1, href: "/proposals/1", title: "The last step" },
  { n: 2, href: "/proposals/2", title: "Ask your agent" },
  { n: 3, href: "/proposals/3", title: "The workspace" },
  { n: 4, href: "/proposals/4", title: "Today vs. shpyrd" },
  { n: 5, href: "/proposals/5", title: "The chat" },
];

export const sharingProposals = [
  { n: 1, href: "/proposals/sharing/1", title: "One sentence" },
  { n: 2, href: "/proposals/sharing/2", title: "Say it your way" },
  { n: 3, href: "/proposals/sharing/3", title: "You say, they see" },
  { n: 4, href: "/proposals/sharing/4", title: "Try it" },
];

const sets = {
  home: { label: "Homepage", items: proposals, current: { href: "/", title: "Current homepage" } },
  sharing: {
    label: "How sharing works",
    items: sharingProposals,
    current: { href: "/how-sharing-works", title: "Current page" },
  },
};

export function ProposalNav({
  set = "home",
  current,
}: {
  set?: keyof typeof sets;
  current?: number;
}) {
  const { label, items, current: today } = sets[set];
  return (
    <nav
      aria-label={`${label} proposals`}
      className="flex flex-wrap items-center gap-x-1 gap-y-2 rounded-lg border border-dashed px-3 py-2 text-sm"
    >
      <span className="mr-2 text-muted-foreground">{label}</span>
      {items.map((p) => (
        <Button
          key={p.n}
          size="sm"
          variant={p.n === current ? "secondary" : "ghost"}
          aria-current={p.n === current ? "page" : undefined}
          asChild
        >
          <Link href={p.href}>
            {p.n} · {p.title}
          </Link>
        </Button>
      ))}
      <Button size="sm" variant="ghost" asChild className="ml-auto">
        <Link href={today.href}>{today.title}</Link>
      </Button>
    </nav>
  );
}

// The page a colleague meets before an app: shpyrd's gate. Orange is access,
// so the one orange thing here is the way in.
export function SignInScreen({ app, audience }: { app: string; audience: string }) {
  return (
    <div className="grid place-items-center bg-muted/40 px-6 py-12">
      <Card className="w-full max-w-80 gap-4 p-6 text-center">
        <LogoMark className="mx-auto size-8" />
        <div className="grid gap-1">
          <p className="font-heading font-medium">Sign in to open {app}</p>
          <p className="text-sm text-muted-foreground">{audience} can open this app.</p>
        </div>
        <Button className="w-full">Use your company account</Button>
      </Card>
    </div>
  );
}

// What someone signed in without a role meets instead of the app. The words
// are the platform's own (docs/app-access): the teams that may open it, never
// the people, and a way to sign in as someone else.
export function DeniedScreen({ app, team }: { app: string; team: string }) {
  return (
    <div className="grid place-items-center bg-muted/40 px-6 py-12">
      <Card className="w-full max-w-80 gap-3 p-6 text-center">
        <LogoMark className="mx-auto size-8" />
        <p className="font-heading font-medium">
          {app} is available to the {team} team
        </p>
        <p className="text-sm text-muted-foreground">
          You are signed in as pedro@acme.com.
        </p>
        <Button variant="outline" className="w-full">
          Sign in as someone else
        </Button>
      </Card>
    </div>
  );
}

// The app itself: the purchase tracker every example on the site is about.
// It is drawn plain on purpose, because it is the team's app and not shpyrd's.
export function PurchaseApp({ person }: { person: string }) {
  const rows = [
    { what: "Laptop for new hire", who: "People", total: "€1,240", state: "Approved" },
    { what: "Van tyres, 4×", who: "Field ops", total: "€620", state: "Waiting" },
    { what: "Trade fair stand", who: "Sales", total: "€3,900", state: "Waiting" },
  ];
  return (
    <div className="grid gap-3 p-5 text-sm">
      <div className="flex items-baseline justify-between gap-4">
        <p className="font-heading font-medium">Purchase requests</p>
        <p className="truncate text-xs text-muted-foreground">Hello, {person}</p>
      </div>
      <ul className="divide-y rounded-lg border">
        {rows.map((r) => (
          <li key={r.what} className="flex items-center gap-3 px-3 py-2">
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{r.what}</span>
              <span className="block text-xs text-muted-foreground">{r.who}</span>
            </span>
            <span className="tabular-nums">{r.total}</span>
            <span className="w-16 text-right text-xs text-muted-foreground">{r.state}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export type LauncherApp = {
  name: string;
  team: string;
  icon: React.ReactElement;
};

export const launcherApps: LauncherApp[] = [
  { name: "Purchase requests", team: "Operations", icon: <Receipt /> },
  { name: "Onboarding checklist", team: "People", icon: <UserPlus /> },
  { name: "Quote tool", team: "Sales", icon: <FileText /> },
  { name: "Field reports", team: "Operations", icon: <ClipboardList /> },
  { name: "Weekly numbers", team: "Finance", icon: <Gauge /> },
];

// What someone sees when they sign in to the workspace: the apps they may
// open, and nothing else.
export function Launcher({
  person,
  apps,
}: {
  person: string;
  apps: LauncherApp[];
}) {
  return (
    <div className="grid gap-4 p-5">
      <div className="flex items-baseline justify-between gap-4">
        <p className="font-heading font-medium">Your apps</p>
        <p className="truncate text-xs text-muted-foreground">Signed in as {person}</p>
      </div>
      <ul className="grid grid-cols-2 gap-3">
        {apps.map((app) => (
          <li key={app.name}>
            <Card size="sm" className="h-full gap-2 px-3 [&_svg]:size-4">
              <span className="text-muted-foreground">{app.icon}</span>
              <span className="grid">
                <span className="text-sm font-medium">{app.name}</span>
                <span className="text-xs text-muted-foreground">{app.team}</span>
              </span>
            </Card>
          </li>
        ))}
      </ul>
    </div>
  );
}

// The end of the sharing proposals: the one thing to do first, and nothing to
// read. Sharing is a sentence to your agent; this is how the agent learns to.
export function ConnectOnce() {
  return (
    // The intro takes the whole width and centres its own words: it sizes its
    // text by its own room, so inside a column that centres (and so shrinks)
    // its children it had the room of one word.
    <div className="grid justify-items-center gap-5 border-t pt-12">
      <SectionIntro
        align="center"
        className="w-full"
        heading="Connect your agent once"
        description="Then sharing is something you say."
      />
      <AddToAgent />
      <a
        href={developerCta.href}
        className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
      >
        Or run it yourself
      </a>
    </div>
  );
}

// The end of the homepage (and the old homepage proposals): the shipyard at
// work beside the way in. shpyrd cloud first, always; running it yourself is
// there too, as the second way, because it is open source and says so.
export function Closing() {
  return (
    <ShipyardCta
      heading="Start on shpyrd cloud"
      description="Sign up, connect your agent, and ship your first app. Or run it yourself: shpyrd is open source under MPL-2.0, and the quick start takes you from nothing to a deployed app."
      actions={
        <>
          <Button asChild>
            <a href="/docs/getting-started">Get started</a>
          </Button>
          <Button variant="outline" asChild>
            <a href="/docs/installation">Run it yourself</a>
          </Button>
        </>
      }
    />
  );
}
