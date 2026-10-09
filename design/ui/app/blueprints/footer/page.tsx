"use client";

import { Globe, Mail } from "lucide-react";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Footer } from "@shpyrd/ui/components/footer";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();
const a = (t: string) => (
  <a key={t} href="#" onClick={stay}>
    {t}
  </a>
);

const columns = [
  { title: "Solutions", links: ["For developers", "For IT teams", "For FDE partners"].map(a) },
  { title: "Use cases", links: ["Internal tools", "Apps from a hackathon", "Agents and workers", "Client apps"].map(a) },
  { title: "Product", links: ["Getting Started", "Pricing", "Docs", "Roadmap"].map(a) },
];

// The gallery has no brand marks of other companies; plain glyphs stand in.
const social = [
  { label: "Our website", href: "#website", icon: <Globe /> },
  { label: "Write to us", href: "#mail", icon: <Mail /> },
];

export default function Page() {
  return (
    <>
      <Section title="The full foot, for a site's main page">
        <div className="overflow-hidden rounded-xl border">
          <Footer
            logo={
              <a href="#" onClick={stay} aria-label="shpyrd">
                <Wordmark className="h-6" />
              </a>
            }
            tagline="You built it. We ship it: online, behind a sign-in, for the people who need it."
            action={<Button size="sm">Get started</Button>}
            columns={columns}
            social={social}
            note="© 2026 shpyrd. Open source under MPL-2.0."
            backToTop
          />
        </div>
      </Section>

      <Section title="In a narrow room everything is stacked">
        <div className="max-w-sm overflow-hidden rounded-xl border">
          <Footer
            logo={<Wordmark className="h-6" />}
            tagline="You built it. We ship it."
            columns={columns}
            social={social}
            note="© 2026 shpyrd."
          />
        </div>
      </Section>
    </>
  );
}
