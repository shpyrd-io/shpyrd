import { AppWindow } from "lucide-react";
import { symbolInk, tileOf, type IconChoice } from "@shpyrd/ui/components/app-icons";
import { Tile } from "@shpyrd/ui/components/launcher-card";
import { inks } from "@shpyrd/ui/lib/chart";
import type { ProjectSummary } from "@/api/types";
import { toneOf } from "@/lib/project";

// How a project is drawn on its card: the symbol or the image it chose,
// in its colour; a window, in the ink its slug keeps, when it chose none.

export function choiceOf(p: ProjectSummary): IconChoice {
  return { icon: p.icon, colour: p.iconColor, file: p.iconUrl ? { src: p.iconUrl, type: p.iconType ?? "" } : undefined };
}

export function lookOf(p: ProjectSummary) {
  const { icon, picture } = tileOf(choiceOf(p));
  return { icon: icon ?? (picture ? undefined : <AppWindow />), picture, colour: p.iconColor, tone: toneOf(p.slug) };
}

// The tile alone, as the heading of the project shows it.
export function ProjectTile({ project, className }: { project: ProjectSummary; className?: string }) {
  const { icon, picture, colour, tone } = lookOf(project);
  return (
    <span className={inks[tone]} style={symbolInk(colour)}>
      <Tile icon={icon} picture={picture} className={className} />
    </span>
  );
}
