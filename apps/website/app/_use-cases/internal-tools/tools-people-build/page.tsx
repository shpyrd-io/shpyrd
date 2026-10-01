import { ClipboardCheck, FileText, Gauge, ListChecks, Receipt, UserPlus } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { InternalToolsNext } from "@/components/use-case-internal-tools";

// Internal tools · Find yours: six kinds of tool, each with the one sentence
// you'd say about it, so a reader finds their own.

export const metadata = {
  title: "Internal tools · Tools people build",
  description: "Whatever you built with AI for your team, sharing it is one sentence to your agent.",
};

const tools = [
  { icon: <Receipt />, kind: "A tracker", say: "Share the purchase tracker with Finance." },
  { icon: <Gauge />, kind: "A dashboard", say: "Let the managers see the weekly numbers. Just look, no changes." },
  { icon: <ClipboardCheck />, kind: "An approval tool", say: "Give the approvals app to the team leads." },
  { icon: <FileText />, kind: "A quote tool", say: "Put the quote tool in front of Sales." },
  { icon: <UserPlus />, kind: "An onboarding checklist", say: "Every new starter should have the onboarding checklist." },
  { icon: <ListChecks />, kind: "Field reports", say: "Share field reports with Operations, and let Rui change it." },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="Find your tool, and the sentence that shares it."
        description="Say it to the agent you built it with."
      />

      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {tools.map((t) => (
          <li key={t.kind}>
            <Card className="h-full gap-3 px-5 [&_svg]:size-5">
              <span className="text-muted-foreground">{t.icon}</span>
              <p className="font-heading font-medium">{t.kind}</p>
              <p className="rounded-2xl rounded-br-sm bg-muted px-4 py-2.5 text-sm">“{t.say}”</p>
            </Card>
          </li>
        ))}
      </ul>

      <InternalToolsNext next={{ title: "Who does what", href: "/use-cases/internal-tools/who-does-what" }} />
    </PageLayoutContent>
  );
}
