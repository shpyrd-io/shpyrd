"use client";

// Slide 1: welcome to the Small Software era. Beside it, the apps of a
// company, scattered and then gathered (apps-yard.tsx).
import { ArrowRight, KeyRound, Moon, ShieldCheck, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { KeybindingHint } from "@shpyrd/ui/components/keybinding-hint";
import { opening } from "@shpyrd/content/site/tour";
import { Label, Lead } from "./parts";
import { AppsYard } from "./apps-yard";

const pointIcons = [<KeyRound key="k" />, <Users key="u" />, <Moon key="m" />, <ShieldCheck key="s" />];

export function Opening({ next }: { next: () => void }) {
  return (
    <div className="grid items-center gap-12 lg:grid-cols-[1.1fr_1fr]">
      <div className="grid gap-6">
        <Label>
          <span className="size-1.5 rounded-full bg-primary" />
          {opening.label}
        </Label>
        <h1 className="font-heading text-5xl leading-[1.04] font-semibold tracking-tight text-balance sm:text-6xl">
          {opening.heading[0]} <span className="text-primary">{opening.heading[1]}</span> {opening.heading[2]}
        </h1>
        <Lead>{opening.lead}</Lead>
        <ul className="flex flex-wrap gap-2">
          {opening.points.map((point, i) => (
            <li key={point} className="inline-flex items-center gap-2 rounded-lg border border-border bg-card/70 px-3 py-1.5 text-sm [&_svg]:size-4 [&_svg]:text-primary">
              {pointIcons[i]}
              {point}
            </li>
          ))}
        </ul>
        <div className="flex items-center gap-4">
          <Button size="lg" onClick={next} className="rounded-full" iconEnd={<ArrowRight />}>
            {opening.start}
          </Button>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <KeybindingHint keys="ArrowRight" size="sm" /> or <KeybindingHint keys="Space" size="sm" />
          </span>
        </div>
      </div>

      <AppsYard />
    </div>
  );
}
