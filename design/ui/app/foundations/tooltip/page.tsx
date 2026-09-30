import { Copy, Info, Trash2 } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { KeybindingHint } from "@shpyrd/ui/components/keybinding-hint";
import { Stack } from "@shpyrd/ui/components/stack";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@shpyrd/ui/components/tooltip";
import { Section } from "../../section";

// A tooltip needs a `TooltipProvider` over it, once per application.
export default function Page() {
  return (
    <TooltipProvider>
      <Section title="What a button does, when the button is only an icon">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="icon" icon={<Copy />} aria-label="Copy the address" />
            </TooltipTrigger>
            <TooltipContent>Copy the address</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="destructive" size="icon" icon={<Trash2 />} aria-label="Destroy" />
            </TooltipTrigger>
            <TooltipContent>Destroy the project</TooltipContent>
          </Tooltip>
        </Stack>
      </Section>
      <Section title="With the keys of the shortcut">
        <Stack direction="horizontal" align="center">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline">Deploy</Button>
          </TooltipTrigger>
          <TooltipContent>
            Deploy the folder <KeybindingHint keys="mod d" size="sm" variant="onEmphasis" />
          </TooltipContent>
        </Tooltip>
        </Stack>
      </Section>
      <Section title="On each side">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          {(["top", "right", "bottom", "left"] as const).map((side) => (
            <Tooltip key={side}>
              <TooltipTrigger asChild>
                <Button variant="outline" size="sm">
                  {side}
                </Button>
              </TooltipTrigger>
              <TooltipContent side={side}>On the {side}</TooltipContent>
            </Tooltip>
          ))}
        </Stack>
      </Section>
      <Section title="A word that explains">
        <p className="text-sm">
          Reserved is what the running processes asked for
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="button" className="ml-1 inline-flex align-middle text-muted-foreground" aria-label="What reserved means">
                <Info className="size-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent>It limits how much more can be scheduled, whether it is used or not.</TooltipContent>
          </Tooltip>
        </p>
      </Section>
    </TooltipProvider>
  );
}
