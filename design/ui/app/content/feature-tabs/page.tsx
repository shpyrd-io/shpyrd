"use client";

import { ArrowUpRight, Database, KeyRound, Rocket } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { FeatureTabs } from "@shpyrd/ui/components/feature-grid";
import { IDE } from "@shpyrd/ui/components/ide";
import { Section } from "../../section";
import { deploy, manifest, server } from "../../code-samples";

const docs = (
  <Button variant="outline" size="sm" onClick={(e) => e.preventDefault()}>
    Documentation <ArrowUpRight />
  </Button>
);

const tabs = [
  { id: "deploy", icon: <Rocket />, label: "Deploy", content: <IDE code={deploy} language="sh" />, link: docs },
  { id: "manifest", icon: <Database />, label: "Databases", content: <IDE code={manifest} language="yaml" />, link: docs },
  { id: "auth", icon: <KeyRound />, label: "Sign-in", content: <IDE code={server} language="js" /> },
];

export default function Page() {
  return (
    <>
      <Section title="A list to choose from and a panel for the one chosen">
        <FeatureTabs
          heading="Never write"
          headingAccent="an API again"
          description="Describe what the app needs and shpyrd runs it, signs people in and keeps it up."
          tabs={tabs}
        />
      </Section>
      <Section title="Starting on another one">
        <FeatureTabs
          heading="Never write"
          headingAccent="a login again"
          description="The last tab has no link at the foot of its panel."
          tabs={tabs}
          defaultTab={2}
        />
      </Section>
    </>
  );
}
