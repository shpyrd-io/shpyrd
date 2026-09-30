"use client";

import { useState } from "react";
import { Rocket, RotateCw, ScrollText, Settings, Trash2 } from "lucide-react";
import { DropdownButton } from "@shpyrd/ui/components/dropdown-button";
import {
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@shpyrd/ui/components/dropdown-menu";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  const [done, setDone] = useState("none");
  return (
    <>
      <Section title="A button that opens a menu">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <DropdownButton label="Actions">{menu}</DropdownButton>
          <DropdownButton label="Actions" variant="outline" icon={<Settings />}>
            {menu}
          </DropdownButton>
          <DropdownButton label="Actions" variant="secondary" size="sm">
            {menu}
          </DropdownButton>
          <DropdownButton label="Actions" variant="ghost" align="end">
            {menu}
          </DropdownButton>
          <DropdownButton label="Disabled" variant="outline" disabled>
            {menu}
          </DropdownButton>
        </Stack>
      </Section>
      <Section title="With a default action">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <DropdownButton label="Deploy" icon={<Rocket />} onClick={() => setDone("Deploy")}>
            {menu}
          </DropdownButton>
          <DropdownButton label="Deploy" variant="outline" onClick={() => setDone("Deploy")}>
            {menu}
          </DropdownButton>
          <DropdownButton
            label="Restart"
            variant="secondary"
            size="sm"
            icon={<RotateCw />}
            onClick={() => setDone("Restart")}
          >
            {menu}
          </DropdownButton>
          <DropdownButton
            label="Destroy"
            variant="destructive"
            onClick={() => setDone("Destroy")}
          >
            {menu}
          </DropdownButton>
          <DropdownButton label="Disabled" disabled onClick={() => setDone("Disabled")}>
            {menu}
          </DropdownButton>
        </Stack>
        <p className="text-sm text-muted-foreground">The last one done: {done}</p>
      </Section>
    </>
  );
}

const menu = (
  <>
    <DropdownMenuLabel>Hello World</DropdownMenuLabel>
    <DropdownMenuItem>
      <RotateCw />
      Restart the instances
    </DropdownMenuItem>
    <DropdownMenuItem>
      <ScrollText />
      See the logs
    </DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuItem variant="destructive">
      <Trash2 />
      Destroy the project
    </DropdownMenuItem>
  </>
);
