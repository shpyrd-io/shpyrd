import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { boundariesSection, developerSection, hosting, pillars, situation, useCases } from "@shpyrd/content/site/home";
import { active } from "@shpyrd/content/site/messages";
import { offer, secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Boundaries } from "@/components/boundaries";
import { Roster } from "@/components/roster";

// The last row is the hook: the reader's own app, the one that works and that
// nobody else can open yet.
const workspace = [
  { name: "Purchase requests", audience: "Finance, Operations", shared: true },
  { name: "Onboarding checklist", audience: "People", shared: true },
  { name: "Quote tool", audience: "Sales", shared: true },
  { name: "Field reports", audience: "Not shared yet", shared: false },
];

export const metadata = {
  title: "shpyrd - One place to share apps with your team",
  description: active.explanation,
};

export default function Home() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Stack gap="normal">
        <h1 className="max-w-[18ch] text-4xl font-semibold tracking-tight sm:text-5xl">
          {active.headline}
        </h1>
        <p className="max-w-prose text-lg text-muted-foreground">{active.explanation}</p>
        <Stack direction="horizontal" gap="cozy" align="center">
          <AddToAgent />
          <Button variant="outline" asChild>
            <a href={secondaryCta.href}>{secondaryCta.label}</a>
          </Button>
        </Stack>
        {active.supporting && (
          <p className="max-w-prose border-t pt-4 text-sm text-muted-foreground">
            {active.supporting}
          </p>
        )}
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{situation.title}</h2>
        {situation.body.map((paragraph) => (
          <p key={paragraph} className="max-w-prose">
            {paragraph}
          </p>
        ))}
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{pillars.title}</h2>
        <div className="grid gap-8 lg:grid-cols-[1fr_20rem]">
          <dl className="grid gap-8 sm:grid-cols-2">
            {pillars.items.map((item) => (
              <div key={item.id}>
                <dt className="font-semibold">{item.title}</dt>
                <dd className="mt-2 text-muted-foreground">{item.body}</dd>
              </div>
            ))}
          </dl>
          <Roster caption="Your workspace" entries={workspace} />
        </div>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{hosting.title}</h2>
        {hosting.body.map((paragraph) => (
          <p key={paragraph} className="max-w-prose text-muted-foreground">
            {paragraph}
          </p>
        ))}
        <Button variant="link" asChild className="justify-self-start px-0">
          <a href={hosting.link.href}>{hosting.link.label}</a>
        </Button>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{useCases.title}</h2>
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {useCases.examples.map((example) => (
            <li key={example} className="rounded-lg border px-4 py-6 font-medium">
              {example}
            </li>
          ))}
        </ul>
        <p className="max-w-prose text-muted-foreground">{useCases.disclaimer}</p>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.title}</h2>
        <p className="max-w-prose text-muted-foreground">{offer.intro}</p>
        <Button variant="outline" asChild className="justify-self-start">
          <a href="/bring-an-app">Read what a session is</a>
        </Button>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{boundariesSection.title}</h2>
        <p className="max-w-prose text-muted-foreground">{boundariesSection.intro}</p>
        <Boundaries items={boundaries} />
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{developerSection.title}</h2>
        <p className="max-w-prose text-muted-foreground">{developerSection.body}</p>
        <pre className="max-w-prose overflow-x-auto rounded-lg border bg-muted p-4 text-sm">
          <code>{developerSection.code}</code>
        </pre>
        <Stack direction="horizontal" gap="cozy">
          {developerSection.links.map((link) => (
            <Button key={link.href} variant="outline" asChild>
              <a href={link.href}>{link.label}</a>
            </Button>
          ))}
        </Stack>
      </Stack>
    </PageLayoutContent>
  );
}
