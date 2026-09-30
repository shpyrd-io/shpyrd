"use client";

import { useState } from "react";
import { Label } from "@shpyrd/ui/components/label";
import { Stack } from "@shpyrd/ui/components/stack";
import { Switch } from "@shpyrd/ui/components/switch";
import { Section } from "../../section";

export default function Page() {
  const [loading, setLoading] = useState(false);
  const [mail, setMail] = useState(false);
  return (
    <>
      <Section title="On and off, with a label">
        <Stack gap="normal" className="max-w-sm">
          <Stack direction="horizontal" align="center" justify="space-between" gap="normal">
            <Stack gap="tight">
              <Label htmlFor="password">Email and password sign-in</Label>
              <span className="text-xs text-muted-foreground">Takes effect at once.</span>
            </Stack>
            <Switch id="password" defaultChecked />
          </Stack>
          <Stack direction="horizontal" align="center" justify="space-between" gap="normal">
            <Label htmlFor="metrics">Metrics</Label>
            <Switch id="metrics" />
          </Stack>
        </Stack>
      </Section>
      <Section title="The word at the end, or none">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Switch aria-label="Backups" defaultChecked statusLabelPosition="end" />
          <Switch aria-label="Backups" defaultChecked statusLabel={false} />
        </Stack>
      </Section>
      <Section title="Small">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Switch aria-label="Drains" size="sm" defaultChecked />
          <Switch aria-label="Drains" size="sm" />
        </Stack>
      </Section>
      <Section title="While it takes effect, and disabled">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Switch
            aria-label="Mail"
            checked={mail}
            loading={loading}
            onCheckedChange={(next) => {
              setLoading(true);
              setTimeout(() => {
                setMail(next);
                setLoading(false);
              }, 1200);
            }}
          />
          <Switch aria-label="Registry" disabled defaultChecked />
          <Switch aria-label="Registry" disabled />
        </Stack>
      </Section>
    </>
  );
}
