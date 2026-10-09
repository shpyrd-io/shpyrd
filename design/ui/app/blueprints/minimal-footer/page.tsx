"use client";

import { Globe, Mail } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { MinimalFooter } from "@shpyrd/ui/components/minimal-footer";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const links = ["For developers", "For IT teams", "Getting Started", "Docs", "Roadmap"].map((t) => (
  <a key={t} href="#" onClick={stay}>
    {t}
  </a>
));

// The gallery has no brand marks of other companies; plain glyphs stand in.
const social = [
  { label: "Our website", href: "#website", icon: <Globe /> },
  { label: "Write to us", href: "#mail", icon: <Mail /> },
];

const logo = (
  <a href="#" onClick={stay} aria-label="shpyrd">
    <LogoMark className="size-5" />
  </a>
);

export default function Page() {
  return (
    <>
      <Section title="Links and where else to find you, then the mark and one line">
        <div className="overflow-hidden rounded-xl border">
          <div className="h-16" />
          <MinimalFooter
            links={links}
            social={social}
            logo={logo}
            note="shpyrd is open source under MPL-2.0, and in beta."
            backToTop
          />
        </div>
      </Section>

      <Section title="With footnotes over the rows">
        <div className="overflow-hidden rounded-xl border">
          <MinimalFooter
            links={links}
            logo={logo}
            note="shpyrd is open source under MPL-2.0."
            footnotes="The agents' names and marks belong to their companies."
          />
        </div>
      </Section>

      <Section title="In a narrow room everything is stacked">
        <div className="max-w-sm overflow-hidden rounded-xl border">
          <MinimalFooter links={links} social={social} logo={logo} note="Open source, in beta." backToTop />
        </div>
      </Section>

      <Section title="Only the mark and its line">
        <div className="overflow-hidden rounded-xl border">
          <MinimalFooter logo={logo} note="shpyrd is open source under MPL-2.0, and in beta." />
        </div>
      </Section>
    </>
  );
}
