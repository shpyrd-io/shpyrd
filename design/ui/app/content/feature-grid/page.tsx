"use client";

import { ArrowUpRight, Database, KeyRound, Rocket, RefreshCw, ScrollText, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { FeatureGrid } from "@shpyrd/ui/components/feature-grid";
import { Section } from "../../section";

const features = [
  { icon: <Rocket />, title: "One-line deploys", description: "Push the code your team already has and it is online." },
  { icon: <KeyRound />, title: "Sign-in built in", description: "Every app sits behind your company's sign-in." },
  { icon: <Database />, title: "Databases", description: "A managed database, backed up and ready." },
  { icon: <RefreshCw />, title: "Rollbacks", description: "Go back to any numbered release in one click." },
  { icon: <ScrollText />, title: "Logs", description: "Every line from every process, searchable." },
  { icon: <Users />, title: "Sharing", description: "Choose which groups can open each app." },
];

const action = (
  <Button variant="outline" onClick={(e) => e.preventDefault()}>
    Explore features <ArrowUpRight />
  </Button>
);

export default function Page() {
  return (
    <>
      <Section title="Heading in two tones, an action, and the features two across">
        <FeatureGrid
          heading="Powerful features"
          headingAccent="Scale to millions"
          description="Everything a team needs to run an internal app, without running the platform under it."
          action={action}
          features={features}
        />
      </Section>
      <Section title="Without an action, three features">
        <FeatureGrid
          heading="Built for teams"
          headingAccent="Not for ops"
          description="Less to set up, less to keep."
          features={features.slice(0, 3)}
        />
      </Section>
    </>
  );
}
