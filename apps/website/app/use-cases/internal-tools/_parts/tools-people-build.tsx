import { ClipboardCheck, FileText, Gauge, ListChecks, Receipt, UserPlus } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { pageSections } from "@/lib/page";

// Internal tools · Find yours: six kinds of tool, each with the one sentence
// you'd say about it, so a reader finds their own.

const tools = [
  { icon: <Receipt />, kind: "A tracker", say: "Share the purchase tracker with Finance." },
  { icon: <Gauge />, kind: "A dashboard", say: "Let the managers see the weekly numbers. Just look, no changes." },
  { icon: <ClipboardCheck />, kind: "An approval tool", say: "Give the approvals app to the team leads." },
  { icon: <FileText />, kind: "A quote tool", say: "Put the quote tool in front of Sales." },
  { icon: <UserPlus />, kind: "An onboarding checklist", say: "Every new starter should have the onboarding checklist." },
  { icon: <ListChecks />, kind: "Field reports", say: "Share field reports with Operations, and let Rui change it." },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2" align="center"
        variant="page"
        heading="Find your tool, and the sentence that shares it."
        description="Say it to the agent you built it with."
      />

      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {tools.map((t) => (
          <li key={t.kind}>
            <Card className={cn(glass, "h-full gap-3 px-5 [&_svg]:size-5")}>
              <span className="text-muted-foreground">{t.icon}</span>
              <p className="font-heading font-medium">{t.kind}</p>
              <p className="rounded-2xl rounded-br-sm bg-muted px-4 py-2.5 text-sm">“{t.say}”</p>
            </Card>
          </li>
        ))}
      </ul>

    </PageLayoutContent>
  );
}
