"use client";

import { Button } from "@shpyrd/ui/components/button";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Checklist, ChecklistItems } from "@shpyrd/ui/components/checklist";
import { GitBranch } from "lucide-react";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const items = [
  "A copy of your app for every change",
  "Its own data, cleared when it is merged",
  "A link to share it with your team",
  "Rolled back with one click",
];

function Preview() {
  return (
    <AppWindow
      title={
        <span className="inline-flex items-center gap-1.5">
          <GitBranch className="size-3.5" />
          preview/new-invoice
        </span>
      }
      className="h-full rounded-tr-none rounded-br-none rounded-bl-none border-r-0 border-b-0 shadow-none"
    >
      <div className="grid gap-2 p-4 text-sm">
        <div className="h-3 w-2/3 rounded bg-muted" />
        <div className="h-3 w-1/2 rounded bg-muted" />
        <div className="h-3 w-3/4 rounded bg-primary/30" />
        <div className="h-3 w-1/3 rounded bg-muted" />
      </div>
    </AppWindow>
  );
}

export default function Page() {
  return (
    <>
      <Section title="A card with its checks and a way to learn more">
        <div className="max-w-lg">
          <Checklist
            heading="Branching"
            items={items}
            action={
              <Button variant="outline" onClick={stay}>
                Learn more
              </Button>
            }
          />
        </div>
      </Section>

      <Section title="With a picture that runs to the edge of the card">
        <Checklist
          heading="Branching"
          items={items}
          action={
            <Button variant="outline" onClick={stay}>
              Learn more
            </Button>
          }
          picture={<Preview />}
        />
      </Section>

      <Section title="Two side by side">
        <div className="grid gap-6 @3xl:grid-cols-2">
          <Checklist heading="Previews" items={items.slice(0, 3)} action={<Button variant="outline" onClick={stay}>Learn more</Button>} />
          <Checklist heading="Releases" items={items.slice(1)} action={<Button variant="outline" onClick={stay}>Learn more</Button>} />
        </div>
      </Section>

      <Section title="The list alone, for use outside a card">
        <ChecklistItems items={items} />
      </Section>
    </>
  );
}
