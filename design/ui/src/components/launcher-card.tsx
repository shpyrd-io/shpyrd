import * as React from "react";
import { cn } from "cn";
import { Bot, Globe, Lock, Moon, Network, Settings } from "lucide-react";
import { Badge } from "./badge";
import { Button } from "./button";
import { Card, CardFooter } from "./card";
import { StatusBadge } from "./status-badge";
import { inks, type Tone } from "../lib/chart";
import { symbolInk } from "./app-icons";

// One application, as the people who open it see it: what it is, how it
// is, where it answers. The whole card opens it; one without a web
// process answers nowhere and only tells. Every card is the same height:
// a name is cut at its line, a description at its second, the tags at
// their first row, and the band stays at the bottom.

export type Phase = "running" | "deploying" | "failed" | "sleeping";
export type Exposure = "public" | "internal";
export type Access = "open" | "locked";

export const phases: Record<Phase, { label: string; dot: string; live?: boolean }> = {
  running: { label: "Running", dot: "bg-success" },
  deploying: { label: "Deploying", dot: "bg-warning", live: true },
  failed: { label: "Failed", dot: "bg-destructive" },
  sleeping: { label: "Sleeping, wakes on the first request", dot: "bg-muted-foreground" },
};

export const exposures: Record<Exposure, { label: string; icon: React.ReactElement }> = {
  public: { label: "Public internet", icon: <Globe /> },
  internal: { label: "Local network", icon: <Network /> },
};

// The light beside the name: how the application is, at a glance.
function Semaphore({ phase, className }: { phase: Phase; className?: string }) {
  const p = phases[phase];
  return phase === "sleeping" ? (
    <Moon
      data-slot="launcher-semaphore"
      data-phase={phase}
      aria-label={p.label}
      className={cn("size-3.5 shrink-0 text-muted-foreground", className)}
    />
  ) : (
    <span
      data-slot="launcher-semaphore"
      data-phase={phase}
      role="img"
      aria-label={p.label}
      title={p.label}
      className={cn("inline-block size-2 shrink-0 rounded-full", p.dot, p.live && "animate-pulse", className)}
    />
  );
}

// The icon of the application, on a wash of its own colour; or a picture
// of its own, which fills the tile as it is.
function Tile({ icon, picture, className }: { icon?: React.ReactElement; picture?: string; className?: string }) {
  return (
    <span
      data-slot="launcher-tile"
      data-picture={picture ? true : undefined}
      className={cn(
        "inline-flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-lg [&_svg]:size-6",
        picture ? "bg-muted" : "bg-(--ink)/15 text-(--ink)",
        className,
      )}
    >
      {picture ? <img src={picture} alt="" className="size-full object-cover" /> : icon}
    </span>
  );
}

// Where it answers, and whether it is locked: icons only, each with its
// words for the pointer and for who cannot see.
function Signs({
  exposure,
  access,
  className,
}: {
  exposure: Exposure;
  access: Access;
  className?: string;
}) {
  const where = exposures[exposure];
  return (
    <span
      data-slot="launcher-signs"
      className={cn("inline-flex items-center gap-2 text-muted-foreground [&_svg]:size-4", className)}
    >
      <span role="img" aria-label={where.label} title={where.label} className="inline-flex">
        {where.icon}
      </span>
      {access === "locked" && (
        <span role="img" aria-label="Only for who signs in" title="Only for who signs in" className="inline-flex">
          <Lock />
        </span>
      )}
    </span>
  );
}

// The gear: a real button, with its border, beside the signs.
function Gear({ name, onClick, className }: { name: string; onClick: () => void; className?: string }) {
  return (
    <Button
      variant="outline"
      size="icon-xs"
      icon={<Settings />}
      data-slot="launcher-gear"
      aria-label={`Settings of ${name}`}
      title="Settings"
      onClick={onClick}
      className={cn("relative z-10 text-muted-foreground", className)}
    />
  );
}

function LauncherCard({
  className,
  name,
  description,
  icon,
  picture,
  tone = "orange",
  colour,
  url,
  href,
  phase = "running",
  exposure = "public",
  access = "open",
  tags,
  onSettings,
  size = "default",
  style,
  ...props
}: Omit<React.ComponentProps<typeof Card>, "children"> & {
  name: string;
  description?: React.ReactNode;
  icon?: React.ReactElement;
  // A picture of its own, in place of the icon: shown as it is.
  picture?: string;
  tone?: Tone;
  // The colour the project chose for its symbol, one of symbolColours;
  // it wins over the tone.
  colour?: string;
  // Where it answers. Without one it has no web process: nothing to open,
  // and the icon gives way to the bot.
  url?: string;
  // Where the card goes; by default, https:// and the address.
  href?: string;
  phase?: Phase;
  exposure?: Exposure;
  access?: Access;
  // The words that mark it; on the launcher, the teams that have access
  // to it.
  tags?: string[];
  // With it, a gear at the top opens the settings of the application.
  onSettings?: () => void;
  // `sm` is the card in a row of others, where it is one of many: smaller,
  // without its description and its tags.
  size?: "default" | "sm";
}) {
  const small = size === "sm";
  const opens = url !== undefined;
  return (
    <Card
      data-slot="launcher-card"
      data-phase={phase}
      data-size={size}
      data-exposure={exposure}
      data-access={access}
      data-worker={!opens || undefined}
      className={cn(
        small ? "relative h-36 w-48 shrink-0 gap-2 overflow-hidden [--card-spacing:--spacing(4)] transition-shadow" : "relative h-60 w-64 gap-3 overflow-hidden [--card-spacing:--spacing(5)] transition-shadow",
        opens && "cursor-pointer hover:ring-primary focus-within:ring-primary",
        // A worker is not a door: no ring, a border of its own colour.
        !opens && "border-2 border-(--ink)/40 bg-(--ink)/5 ring-0",
        phase === "sleeping" && "[&_[data-slot=launcher-tile]]:opacity-60",
        inks[tone],
        className,
      )}
      style={{ ...symbolInk(colour), ...style }}
      {...props}
    >
      <div className="flex items-start justify-between gap-3 px-(--card-spacing)">
        {/* The light sits on the corner of the icon. A worker is always the bot: its own icon is not drawn. */}
        <span className="relative">
          <Tile icon={opens ? icon : <Bot />} picture={opens ? picture : undefined} className={small ? "size-9 [&_svg]:size-5" : undefined} />
          <Semaphore
            phase={phase}
            className={cn(
              "absolute -top-1 -right-1 rounded-full ring-2 ring-card",
              phase === "sleeping" ? "bg-card p-px" : "size-2.5",
              !opens && "ring-[color-mix(in_oklab,var(--ink)_5%,var(--card))]",
            )}
          />
        </span>
        {/* The corner is only for who manages it; the signs are in the band. */}
        <div className="flex items-center gap-2">
          {/* A web application that is not well says so in words: the light
              alone is easy to miss. A worker says it in its band. */}
          {opens && (phase === "failed" || phase === "deploying") && (
            <StatusBadge data-slot="launcher-trouble" type={phase === "failed" ? "error" : "warning"} live={phase === "deploying"}>
              {phase === "failed" ? "Not working" : "Updating"}
            </StatusBadge>
          )}
          {onSettings && <Gear name={name} onClick={onSettings} />}
        </div>
      </div>
      <div className="grid gap-1 px-(--card-spacing)">
        <div className={cn("truncate font-heading font-semibold", small ? "text-base" : "text-lg")}>{name}</div>
        {description && !small && <p className="line-clamp-2 text-sm text-muted-foreground">{description}</p>}
      </div>
      {tags && tags.length > 0 && !small && (
        <div className="flex max-h-6 flex-wrap gap-1 overflow-hidden px-(--card-spacing)">
          {tags.map((tag) => (
            <Badge key={tag} variant="secondary">
              {tag}
            </Badge>
          ))}
        </div>
      )}
      {/* The band at the bottom: where it answers or, for a worker, how it is. */}
      <CardFooter
        data-band
        className={cn(
          "mt-auto justify-center py-2.5 font-mono text-xs text-muted-foreground",
          !opens && "border-(--ink)/20 bg-(--ink)/8",
        )}
      >
        {opens ? (
          // The link covers the whole card: anywhere on it opens the application.
          <>
            {/* How it is reached and who may open it, beside the address they are about. */}
            <Signs exposure={exposure} access={access} className="mr-1.5 shrink-0 gap-1 [&_svg]:size-3.5" />
            <a
              href={href ?? `https://${url}`}
              className="truncate outline-none after:absolute after:inset-0 after:rounded-xl"
            >
              {url}
            </a>
          </>
        ) : (
          <span className={cn(phase === "failed" && "font-medium text-destructive")}>{phases[phase].label}</span>
        )}
      </CardFooter>
    </Card>
  );
}

export { Gear, LauncherCard, Semaphore, Signs, Tile };
