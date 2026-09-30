"use client";

import { useState } from "react";
import { Megaphone } from "lucide-react";
import {
  Alert,
  AlertAction,
  AlertActions,
  AlertDescription,
  AlertTitle,
} from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [dismissed, setDismissed] = useState(false);
  return (
    <>
      <Section title="Kinds">
        <Stack gap="cozy">
          <Alert>
            <AlertTitle>The cluster is being upgraded</AlertTitle>
            <AlertDescription>Deploys wait until it ends.</AlertDescription>
          </Alert>
          <Alert variant="info">
            <AlertTitle>A new region is available</AlertTitle>
            <AlertDescription>
              Projects can now run in São Paulo. <a href="#regions">See the regions</a>.
            </AlertDescription>
          </Alert>
          <Alert variant="success">
            <AlertTitle>The release v12 is out</AlertTitle>
            <AlertDescription>All four instances are running it.</AlertDescription>
          </Alert>
          <Alert variant="warning">
            <AlertTitle>The disk is almost full</AlertTitle>
            <AlertDescription>186 of 200 GiB are taken. Logs older than a week can go.</AlertDescription>
          </Alert>
          <Alert variant="destructive">
            <AlertTitle>The worker could not start</AlertTitle>
            <AlertDescription>
              It exited with code 1 three times in a row. <a href="#logs">Read the logs</a>.
            </AlertDescription>
          </Alert>
          <Alert variant="upsell">
            <AlertTitle>More room on the Team plan</AlertTitle>
            <AlertDescription>Ten projects, and a member for each of them.</AlertDescription>
          </Alert>
        </Stack>
      </Section>
      <Section title="With what can be done">
        <Stack gap="cozy">
          <Alert variant="warning">
            <AlertTitle>The disk is almost full</AlertTitle>
            <AlertDescription>186 of 200 GiB are taken.</AlertDescription>
            <AlertActions>
              <Button size="sm">Clear the logs</Button>
              <Button size="sm" variant="outline">
                Add a volume
              </Button>
            </AlertActions>
          </Alert>
          <Alert variant="info">
            <AlertTitle>A new version of the CLI is out</AlertTitle>
            <AlertAction>
              <Button size="xs" variant="outline">
                Update
              </Button>
            </AlertAction>
          </Alert>
        </Stack>
      </Section>
      <Section title="That can be dismissed">
        {dismissed ? (
          <Button size="sm" variant="outline" onClick={() => setDismissed(false)}>
            Show it again
          </Button>
        ) : (
          <Alert variant="success" onDismiss={() => setDismissed(true)}>
            <AlertTitle>The domain is verified</AlertTitle>
            <AlertDescription>Requests to shpyrd.io reach the project.</AlertDescription>
          </Alert>
        )}
      </Section>
      <Section title="An icon of its own, or none">
        <Stack gap="cozy">
          <Alert variant="upsell" icon={<Megaphone />}>
            <AlertTitle>Workspaces are here</AlertTitle>
            <AlertDescription>Projects and people, together under one bill.</AlertDescription>
          </Alert>
          <Alert variant="info" icon={null}>
            <AlertTitle>Only the words</AlertTitle>
            <AlertDescription>Without the icon, the text starts at the edge.</AlertDescription>
          </Alert>
        </Stack>
      </Section>
    </>
  );
}
