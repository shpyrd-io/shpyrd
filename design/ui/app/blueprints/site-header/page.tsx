"use client";

import { Moon, Search } from "lucide-react";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { SiteHeader } from "@shpyrd/ui/components/site-header";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const links = [
  <a key="sharing" href="#" onClick={stay} aria-current="page">Getting Started</a>,
  <a key="bring" href="#" onClick={stay}>Bring an app</a>,
  <a key="docs" href="#" onClick={stay}>Docs</a>,
];

const grouped = [
  {
    label: "Solutions",
    links: [
      <a key="dev" href="#" onClick={stay}>For developers</a>,
      <a key="it" href="#" onClick={stay}>For IT teams</a>,
      <a key="fde" href="#" onClick={stay}>For FDE partners</a>,
    ],
  },
  ...links,
];

const columned = [
  {
    label: "Solutions",
    columns: [
      {
        label: "For",
        links: [
          <a key="dev" href="#" onClick={stay}>Developers</a>,
          <a key="it" href="#" onClick={stay}>IT teams</a>,
        ],
      },
      {
        label: "What you're shipping",
        links: [
          <a key="tools" href="#" onClick={stay}>Internal tools</a>,
          <a key="agents" href="#" onClick={stay}>Agents and workers</a>,
        ],
      },
    ],
  },
  ...links,
];

const brand = (
  <a href="#" onClick={stay} aria-label="shpyrd">
    <Wordmark />
  </a>
);

const actions = (
  <>
    <Button variant="ghost" size="icon" icon={<Search />} aria-label="Search" />
    <Button variant="ghost" size="icon" icon={<Moon />} aria-label="Theme" />
    <Button className="ml-2 hidden @md/site-header:inline-flex">Get started</Button>
  </>
);

export default function Page() {
  return (
    <>
      <Section title="The brand, the pages, and what to do; the open page is marked">
        <div className="overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} start={brand} links={links} actions={actions} />
          <div className="h-24" />
        </div>
      </Section>

      <Section title="Its contents held to the width of the page's content, the bar still edge to edge">
        <div className="overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} width="medium" start={brand} links={links} actions={actions} />
          <div className="mx-auto h-24 max-w-3xl px-4 py-4 text-sm text-muted-foreground @3xl:px-6">
            The page&apos;s content starts here, under the brand.
          </div>
        </div>
      </Section>

      <Section title="In a narrow room the links fold into a menu, and the actions stay">
        <div className="max-w-sm overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} start={brand} links={links} actions={actions} />
          <div className="h-24" />
        </div>
      </Section>

      <Section title="Pages that belong together under one name open as a menu; folded, they are a group">
        <div className="overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} start={brand} links={grouped} actions={actions} />
          <div className="h-40" />
        </div>
      </Section>

      <Section title="Several kinds of pages under one name: a column each, with its heading">
        <div className="overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} start={brand} links={columned} actions={actions} />
          <div className="h-44" />
        </div>
      </Section>

      <Section title="With no links, a brand and its actions">
        <div className="overflow-hidden rounded-xl border">
          <SiteHeader sticky={false} start={brand} actions={actions} />
        </div>
      </Section>
    </>
  );
}
