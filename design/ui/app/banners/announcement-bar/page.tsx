"use client";

import { useState } from "react";
import { ArrowRight } from "lucide-react";
import { AnnouncementBar } from "@shpyrd/ui/components/announcement-bar";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [dismissed, setDismissed] = useState(false);
  return (
    <>
      <Section title="The DevHunt welcome: with the mark and a link">
        <AnnouncementBar
          icon={<LogoMark surface="dark" />}
          action={
            <a href="#signup">
              Get early access
              <ArrowRight />
            </a>
          }
        >
          <strong>Welcome users from DevHunt.</strong> We are looking for early users to get feedback on how to improve.
        </AnnouncementBar>
      </Section>
      <Section title="Grounds: black, white and orange">
        <Stack gap="cozy">
          {(["dark", "light", "orange"] as const).map((ground) => (
            <AnnouncementBar
              key={ground}
              variant={ground}
              icon={<LogoMark surface={ground} />}
              action={
                <a href="#signup">
                  Get early access
                  <ArrowRight />
                </a>
              }
            >
              <strong>Welcome users from DevHunt.</strong> We are looking for early users to get feedback on how to improve.
            </AnnouncementBar>
          ))}
        </Stack>
      </Section>
      <Section title="Only the sentence">
        <AnnouncementBar>
          <strong>shpyrd 1.4 is out.</strong> Backups run on their own now.
        </AnnouncementBar>
      </Section>
      <Section title="In a narrow room">
        <div className="max-w-sm">
          <AnnouncementBar
            icon={<LogoMark surface="dark" />}
            action={
              <a href="#signup">
                Get early access
                <ArrowRight />
              </a>
            }
          >
            <strong>Welcome users from DevHunt.</strong> We are looking for early users to get feedback on how to improve.
          </AnnouncementBar>
        </div>
      </Section>
      <Section title="That can be closed">
        {dismissed ? (
          <Stack direction="horizontal">
            <Button size="sm" variant="outline" onClick={() => setDismissed(false)}>
              Show it again
            </Button>
          </Stack>
        ) : (
          <AnnouncementBar onDismiss={() => setDismissed(true)} icon={<LogoMark surface="dark" />}>
            <strong>Maintenance on Sunday.</strong> Deploys wait from 02:00 to 03:00 UTC.
          </AnnouncementBar>
        )}
      </Section>
    </>
  );
}
