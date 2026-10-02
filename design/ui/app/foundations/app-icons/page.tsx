"use client";

import { useState } from "react";
import { AppIcon, AppSymbol, appIconGroups, symbolColours, symbolInk } from "@shpyrd/ui/components/app-icons";
import { LauncherCard, Tile } from "@shpyrd/ui/components/launcher-card";
import { Section } from "../../section";

const cards = [
  { name: "Corporate", description: "Operations of the day, finance and the team.", icon: "briefcase", colour: "orange", url: "corporate.acme.shpyrd.app", tags: ["Operations"] },
  { name: "Billing", description: "Invoices, plans and what each client owes.", icon: "receipt", colour: "green", url: "billing.acme.shpyrd.app", tags: ["Finance"] },
  { name: "Shop", description: "The store of the agency's own products.", icon: "store", colour: "pink", url: "shop.acme.com", tags: ["Public"] },
  { name: "Fleet", description: "Where every truck is, and what it carries.", icon: "truck", colour: "amber", url: "fleet.acme.internal", tags: ["Logistics"] },
  { name: "Support", description: "What the clients ask, and who answers.", icon: "headset", colour: "violet", url: "support.acme.shpyrd.app", tags: ["Clients"] },
  { name: "Insights", description: "The numbers of the month, for the board.", icon: "chart-line", colour: "blue", url: "insights.acme.internal", tags: ["Board"] },
  { name: "Academy", description: "The courses of the team, and who finished them.", icon: "graduation-cap", colour: "indigo", url: "academy.acme.shpyrd.app", tags: ["People"] },
  { name: "Clinic", description: "Appointments and records of the clinic.", icon: "heart-pulse", colour: "red", url: "clinic.acme.com", tags: ["Health"] },
  { name: "Hiring", description: "Who applied, and where each one is.", icon: "user-round-search", colour: "teal", url: "hiring.acme.shpyrd.app", tags: ["People"] },
  { name: "Harvest", description: "The fields, what was planted and when.", icon: "sprout", colour: "lime", url: "harvest.acme.internal", tags: ["Farm"] },
  { name: "Port", description: "The ships of the week, and their containers.", icon: "ship", colour: "cyan", url: "port.acme.shpyrd.app", tags: ["Logistics"] },
  { name: "Legal", description: "Contracts, and when each one ends.", icon: "scale", colour: "brown", url: "legal.acme.internal", tags: ["Contracts"] },
];

export default function Page() {
  const [colour, setColour] = useState<string>("orange");
  return (
    <>
      <Section title="14 colours, each with a value for each theme, all at 3:1 or more against their tile">
        <div className="flex flex-wrap gap-2">
          {symbolColours.map((name) => (
            <button
              key={name}
              type="button"
              aria-pressed={colour === name}
              onClick={() => setColour(name)}
              className="inline-flex items-center gap-2 rounded-full py-1 pr-3 pl-1 text-sm ring-1 ring-foreground/10 outline-none hover:ring-foreground/30 focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:ring-2 aria-pressed:ring-foreground"
            >
              <span className="size-5 rounded-full" style={{ backgroundColor: `var(--symbol-${name})` }} />
              {name}
            </button>
          ))}
        </div>
      </Section>
      <Section title="64 symbols, in eight groups of what the applications of a business tend to be">
        <div className="grid gap-6" style={symbolInk(colour)}>
          {appIconGroups.map((group) => (
            <div key={group.title} className="grid gap-3">
              <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{group.title}</h3>
              <div className="grid grid-cols-4 gap-x-2 gap-y-4 @3xl/page-layout:grid-cols-8">
                {group.icons.map(([name]) => (
                  <div key={name} className="grid min-w-0 justify-items-center gap-1.5" title={name}>
                    <Tile icon={<AppIcon name={name} />} />
                    <span className="max-w-full truncate font-mono text-[11px] text-muted-foreground">{name}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </Section>
      <Section title="On the launcher: twelve applications, each with its symbol and its colour">
        <div className="flex flex-wrap gap-4">
          {cards.map((card) => (
            <LauncherCard
              key={card.name}
              name={card.name}
              description={card.description}
              icon={<AppIcon name={card.icon} />}
              url={card.url}
              tags={card.tags}
              colour={card.colour}
            />
          ))}
        </div>
      </Section>
      <Section title="A symbol of its own: an SVG the project sent, only its shape, in the colour chosen">
        <div className="flex flex-wrap items-center gap-3">
          {symbolColours.map((name) => (
            <span key={name} style={symbolInk(name)}>
              <Tile icon={<AppSymbol src="/samples/symbol.svg" />} />
            </span>
          ))}
        </div>
      </Section>
      <Section title="A picture of its own, a PNG or a WebP, as it is: no colour is laid on it">
        <div className="flex flex-wrap items-center gap-3">
          <Tile picture="/samples/picture.png" />
          <LauncherCard
            name="Garden"
            description="The orders of the garden centre, and its deliveries."
            picture="/samples/picture.png"
            url="garden.acme.com"
            tags={["Orders"]}
          />
        </div>
      </Section>
    </>
  );
}
