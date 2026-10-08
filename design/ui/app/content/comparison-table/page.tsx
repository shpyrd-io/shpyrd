"use client";

import { ComparisonTable, type ComparisonRow } from "@shpyrd/ui/components/comparison-table";
import { Section } from "../../section";

const link = (text: string) => (
  <a href="#" onClick={(e) => e.preventDefault()}>
    {text}
  </a>
);

const rows: ComparisonRow[] = [
  {
    label: "Sign-in",
    shpyrd: { score: 5, description: <>Every app sits behind your company&apos;s sign-in. {link("How it works")}</> },
    competitors: [
      { score: 2, description: "You build it into each app." },
      { score: 3, description: <>Available on paid plans. {link("Pricing")}</> },
      { score: 1, description: "Not offered." },
    ],
  },
  {
    label: "Databases",
    shpyrd: { score: 5, description: "A managed database in one line of the manifest." },
    competitors: [
      { score: 4, description: "Managed, billed on its own." },
      { score: 3, description: "Managed, with limits on size." },
      { score: 2, description: "You bring your own." },
    ],
  },
  {
    label: "Rollbacks",
    shpyrd: { score: 4, description: "Any release, in one click." },
    competitors: [
      { score: 4, description: "Any release, from the CLI." },
      { score: 2, description: "The last release only." },
      { score: 3, description: "Any release, from the dashboard." },
    ],
  },
];

const take = (n: number): ComparisonRow[] =>
  rows.map((r) => ({ ...r, competitors: r.competitors.slice(0, n) }));

export default function Page() {
  return (
    <>
      <Section title="Against one">
        <ComparisonTable competitors={["Hostly"]} rows={take(1)} />
      </Section>
      <Section title="Against two">
        <ComparisonTable competitors={["Hostly", "Deployr"]} rows={take(2)} />
      </Section>
      <Section title="Against three, with its own heading for the names">
        <ComparisonTable competitors={["Hostly", "Deployr", "Cloudbox"]} corner="Platform comparison" rows={take(3)} />
      </Section>
    </>
  );
}
