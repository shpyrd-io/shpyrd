"use client";

import { Settings } from "lucide-react";
import {
  AnchoredOverlay,
  AnchoredOverlayClose,
} from "@shpyrd/ui/components/anchored-overlay";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <AnchoredOverlay anchor={<Button variant="outline">Toggle overlay</Button>}>
            {overlay}
          </AnchoredOverlay>
        </Stack>
      </Section>
      <Section title="Width, height and place">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <AnchoredOverlay
            anchor={<Button variant="outline">Small and short</Button>}
            width="small"
            height="xsmall"
          >
            {overlay}
          </AnchoredOverlay>
          <AnchoredOverlay
            anchor={<Button variant="outline">To the right</Button>}
            side="right"
            align="end"
            sideOffset={20}
          >
            {overlay}
          </AnchoredOverlay>
        </Stack>
      </Section>
      <Section title="With a form">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <AnchoredOverlay
            anchor={
              <Button variant="outline" icon={<Settings />}>
                Instances
              </Button>
            }
            width="medium"
          >
            <Stack>
              <Field label="Instances">
                <Input inputMode="numeric" defaultValue="2" suffix="of web" />
              </Field>
              <Stack direction="horizontal" justify="end" gap="condensed">
                <AnchoredOverlayClose asChild>
                  <Button variant="outline" size="sm">
                    Cancel
                  </Button>
                </AnchoredOverlayClose>
                <AnchoredOverlayClose asChild>
                  <Button size="sm">Save</Button>
                </AnchoredOverlayClose>
              </Stack>
            </Stack>
          </AnchoredOverlay>
        </Stack>
      </Section>
    </>
  );
}

const overlay = (
  <div className="grid gap-4">
    Anchored overlay content
    <AnchoredOverlayClose asChild>
      <Button variant="outline" className="w-full">
        Close
      </Button>
    </AnchoredOverlayClose>
  </div>
);
