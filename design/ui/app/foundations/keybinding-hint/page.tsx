"use client";

import { RotateCw, ScrollText, Search, Settings } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { DropdownButton } from "@shpyrd/ui/components/dropdown-button";
import { DropdownMenuItem } from "@shpyrd/ui/components/dropdown-menu";
import { Input } from "@shpyrd/ui/components/input";
import { KeybindingHint } from "@shpyrd/ui/components/keybinding-hint";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@shpyrd/ui/components/tooltip";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Keys together, keys in turn">
        <Stack direction="horizontal" wrap="wrap" align="center">
          <KeybindingHint keys="Mod+S" />
          <KeybindingHint keys="g i" />
          <KeybindingHint keys="Mod+Shift+P" />
          <KeybindingHint keys="Control+Alt+Delete" />
          <KeybindingHint keys="Escape" />
          <KeybindingHint keys="ArrowUp" />
          <KeybindingHint keys="/" />
        </Stack>
      </Section>
      <Section title="Format and size">
        <Stack direction="horizontal" wrap="wrap" align="center" className="text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-2">
            Condensed <KeybindingHint keys="Mod+K" />
          </span>
          <span className="inline-flex items-center gap-2">
            Full <KeybindingHint keys="Mod+K" format="full" />
          </span>
          <span className="inline-flex items-center gap-2">
            Small <KeybindingHint keys="Mod+K" size="sm" />
          </span>
        </Stack>
      </Section>
      <Section title="Where it goes">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Button variant="outline" iconEnd={<KeybindingHint keys="Mod+Z" />}>
            Undo
          </Button>
          <Button iconEnd={<KeybindingHint keys="Mod+Enter" variant="onEmphasis" />}>
            Submit
          </Button>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon" icon={<Settings />} aria-label="Settings" />
              </TooltipTrigger>
              <TooltipContent>
                Settings <KeybindingHint keys="g s" variant="onEmphasis" />
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
          <DropdownButton label="Actions" variant="outline">
            <DropdownMenuItem>
              <RotateCw />
              Restart the instances
              <KeybindingHint keys="Mod+R" className="ml-auto pl-4" />
            </DropdownMenuItem>
            <DropdownMenuItem>
              <ScrollText />
              See the logs
              <KeybindingHint keys="g l" className="ml-auto pl-4" />
            </DropdownMenuItem>
          </DropdownButton>
          <Input
            className="max-w-60"
            type="search"
            aria-label="Search"
            icon={<Search />}
            placeholder="Search"
            suffix={<KeybindingHint keys="/" />}
          />
        </Stack>
      </Section>
    </>
  );
}
