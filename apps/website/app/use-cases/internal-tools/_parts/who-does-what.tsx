import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { Boundaries } from "@/components/boundaries";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { pageSections } from "@/lib/page";

// Internal tools · Yours and ours: the research's warning is that people
// expect shpyrd to build the app. This page draws the line: the tool is yours
// and holds the work; shpyrd gives it a place and a door.

const yours = [
  "What the tool does: the requests, the approvals, the numbers",
  "Its own rules, like who can approve over €5,000",
  "The changes you make to it, with your agent",
];

const ours = [
  "An address your colleagues can open",
  "Their work account in front of it, and nobody else inside",
  "Who can use it, and who can change it",
  "Every change kept, so a bad one can be taken back",
];

function List({ title, items }: { title: string; items: string[] }) {
  return (
    <Card className={cn(glass, "gap-3 px-6 py-6")}>
      <p className="font-heading text-lg font-medium">{title}</p>
      <ul className="grid gap-2 text-muted-foreground">
        {items.map((i) => (
          <li key={i} className="border-l-2 pl-3">
            {i}
          </li>
        ))}
      </ul>
    </Card>
  );
}

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2" align="center"
        variant="page"
        heading="What's yours, and what's shpyrd's."
        description="shpyrd doesn't write your app. Your tool keeps doing the work; shpyrd gives it a place on shpyrd cloud."
      />

      <div className="grid gap-4 md:grid-cols-2">
        <List title="Yours" items={yours} />
        <List title="shpyrd's" items={ours} />
      </div>

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge" heading="Worth knowing" />
        <Boundaries items={boundaries.filter((b) => ["sign-in", "rollback"].includes(b.id))} />
      </Stack>

    </PageLayoutContent>
  );
}
