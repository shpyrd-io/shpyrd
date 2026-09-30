"use client";

import { BookOpen, Briefcase, Mail, Tv } from "lucide-react";
import { LauncherCard } from "@shpyrd/ui/components/launcher-card";
import { Section } from "../../section";

const settings = () => {};

export default function Page() {
  return (
    <>
      <Section title="Three applications: one in the open, one asleep behind a lock, a worker">
        <div className="grid items-stretch gap-4 @xl/page-layout:grid-cols-2 @3xl/page-layout:grid-cols-3">
          <LauncherCard
            name="Corporate"
            description="Operations of the day, finance and the team."
            icon={<Briefcase />}
            url="corporate.acme.shpyrd.app"
            tags={["Operations", "Finance"]}
            onSettings={settings}
          />
          <LauncherCard
            name="Dash TV"
            description="The two business units on one screen, for the TV."
            icon={<Tv />}
            tone="violet"
            url="dash.acme.internal"
            phase="sleeping"
            exposure="internal"
            access="locked"
            tags={["Revenue", "Agenda"]}
            onSettings={settings}
          />
          <LauncherCard
            name="Reports"
            description="Builds the reports of the month, every night."
            tone="blue"
            phase="failed"
            exposure="internal"
            tags={["Nightly"]}
            onSettings={settings}
          />
        </div>
      </Section>
      <Section title="Deploying, and the least a card can have">
        <div className="grid items-stretch gap-4 @xl/page-layout:grid-cols-2 @3xl/page-layout:grid-cols-3">
          <LauncherCard
            name="Docs"
            description="What is written about the platform."
            icon={<BookOpen />}
            tone="green"
            url="docs.acme.com"
            phase="deploying"
            tags={["Public"]}
          />
          <LauncherCard name="Mail" icon={<Mail />} tone="neutral" url="mail.acme.internal" exposure="internal" access="locked" />
          <LauncherCard name="Reports" tone="blue" phase="running" exposure="internal" />
        </div>
      </Section>
    </>
  );
}
