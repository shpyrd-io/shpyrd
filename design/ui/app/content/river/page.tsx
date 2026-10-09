"use client";

import { ArrowRight, ChevronRight } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { River } from "@shpyrd/ui/components/river";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

function Picture() {
  return (
    <AppWindow title="Releases" detail="acme-invoices">
      <div className="grid gap-3 p-5 text-sm">
        {["v12  Deployed", "v11  Config changed", "v10  Rolled back"].map((row, i) => (
          <div key={row} className="flex items-center gap-3 border-b pb-3 last:border-0 last:pb-0">
            <span className={i === 0 ? "size-2 rounded-full bg-primary" : "size-2 rounded-full bg-border"} />
            <span className="font-mono text-muted-foreground">{row}</span>
          </div>
        ))}
      </div>
    </AppWindow>
  );
}

const text = {
  heading: "Focus on solving bigger problems",
  description: "Every deploy, config change and rollback is a numbered release, so nobody has to remember what changed or when.",
};

const link = (
  <Button variant="link" size="lg" className="px-0 text-base" iconEnd={<ArrowRight />} onClick={stay}>
    See how releases work
  </Button>
);

export default function Page() {
  return (
    <>
      <Section title="Text at the start, the picture larger beside it">
        <River {...text} link={link} picture={<Picture />} />
      </Section>

      <Section title="Text at the end">
        <River
          {...text}
          align="end"
          link={
            <Button variant="link" size="lg" className="px-0 text-base" iconEnd={<ChevronRight />} onClick={stay}>
              Read more
            </Button>
          }
          picture={<Picture />}
        />
      </Section>

      <Section title="As a card, inside a panel of glass">
        <River variant="card" {...text} link={link} picture={<Picture />} />
      </Section>

      <Section title="One after another, the text on alternate sides">
        <div className="grid gap-20">
          <River as="h3" {...text} heading="Deploy from the code you have" picture={<Picture />} />
          <River as="h3" align="end" {...text} heading="Choose who can use it" picture={<Picture />} />
        </div>
      </Section>
    </>
  );
}
