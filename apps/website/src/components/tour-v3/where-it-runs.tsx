"use client";

// A proposal for slide 8 (Giovani, 2026-10-28). What the slide says: for
// whoever builds nothing changes; what changes is who runs the platform and
// where it, and the data, live. So the scene starts with the one thing that
// is the same, someone shipping an app, and under it the map of where things
// are: your company's line on the left, shpyrd's on the right, which opens
// into one line round everything in your own cloud. Beside it what changes,
// four questions answered for the option shown; pointing at one lights the
// part of the map it is about.
//
// In the manifesto's own look (Giovani, 2026-10-28): the two options chosen
// as slide 6 chooses its people, cards with a route box and a bar filling
// while the slide moves on by itself; the map's boxes as the route's boxes,
// orange when lit; the private links as slide 6's orange thread, drawn again
// on each option; the boxes' headings as slide 6's, an icon in orange; under
// it all the slides' controls.
import { useEffect, useState } from "react";
import {
  ArrowRight,
  Building2,
  ChevronLeft,
  ChevronRight,
  Cloud,
  Database,
  KeyRound,
  ListChecks,
  Map as MapIcon,
  Pause,
  Play,
  Rocket,
  Server,
  Wallet,
  Waypoints,
  Wrench,
} from "lucide-react";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { cn } from "@shpyrd/ui/lib/cn";
import { delivery } from "@/content/tour-v3";
import { PersonMark } from "./person-mark";
import { Intro, Label, panel, rise, mouseOnly } from "./parts";

type Where = "cloud" | "own";
type Part = "build" | "platform" | "data";

// How long each option stays before the next, while the slide moves.
const HOLD = 7000;
const stepButton = "hover:border-primary/40 hover:text-primary";

// The two options, as slide 6's people: a mark, a name with its badge, a
// line, and what it gives at once.
const options: {
  id: Where;
  icon: React.ReactElement;
  badge: string;
  line: string;
  status: string;
}[] = [
  {
    id: "cloud",
    icon: <Cloud />,
    badge: "Managed",
    line: "We run it for you",
    status: "Ready today",
  },
  {
    id: "own",
    icon: <Building2 />,
    badge: "Self-hosted",
    line: "In your Kubernetes, on your account",
    status: "Open source",
  },
];

// What changes, as four questions, each tied to the part of the map it is
// about. From the slide's points (content/tour-v3.ts, delivery.options).
const changes: {
  icon: React.ReactElement;
  question: string;
  part?: Part;
  answer: Record<Where, string>;
}[] = [
  {
    icon: <Rocket />,
    question: "How you start",
    part: "build",
    answer: {
      cloud: "Today: connect your sign-in and ship the first app",
      own: "Install shpyrd in your own Kubernetes, on your account",
    },
  },
  {
    icon: <Wrench />,
    question: "Who runs the platform",
    part: "platform",
    answer: {
      cloud: "We do: updates and scaling included",
      own: "Your team does, with the code open to read",
    },
  },
  {
    icon: <Database />,
    question: "Where your data lives",
    part: "data",
    answer: {
      cloud: "With your apps on shpyrd; your systems over a private link",
      own: "Inside your network: it never leaves",
    },
  },
  {
    icon: <Wallet />,
    question: "How you pay",
    answer: {
      cloud: "For what runs, nothing for idle",
      own: "A license switches on the enterprise features",
    },
  },
];

// The route's box (slide 4, slide 6): white, a fine ring, lit from above;
// orange when it is the one spoken of.
function RouteBox({
  children,
  lit,
  size = "size-12",
}: {
  children: React.ReactNode;
  lit?: boolean;
  size?: string;
}) {
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-2xl ring-1 transition-[background-color,box-shadow,color] duration-normal ease-move [&_svg]:size-5",
        size,
        lit
          ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
          : "bg-background text-primary ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] dark:bg-card",
      )}
    >
      {children}
    </span>
  );
}

function Box({
  icon,
  children,
  note,
  lit,
}: {
  icon: React.ReactElement;
  children: React.ReactNode;
  note: React.ReactNode;
  lit?: boolean;
}) {
  return (
    <div className="relative z-10 flex items-center gap-3">
      <RouteBox lit={lit}>{icon}</RouteBox>
      <div className="grid min-w-0">
        <span className="text-sm font-medium text-foreground">{children}</span>
        <span className="text-xs text-muted-foreground">{note}</span>
      </div>
    </div>
  );
}

// The private link between two boxes: slide 6's orange thread, a fine solid
// line, drawn from left to right each time the option changes.
function Thread({ at, delay }: { at: Where; delay: number }) {
  return (
    <span aria-hidden="true" className="flex h-12 items-center">
      <span
        key={at}
        className="h-px w-full origin-left bg-primary motion-safe:animate-[tour-fill-x_900ms_var(--ease-move)_both]"
        style={{ animationDelay: `${delay}ms` }}
      />
    </span>
  );
}

// A heading as slide 6's boxes have them: small, muted, an icon in orange.
function BoxHeading({
  icon,
  children,
}: {
  icon: React.ReactElement;
  children: React.ReactNode;
}) {
  return (
    <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground [&_svg]:size-3.5 [&_svg]:text-primary">
      {icon}
      {children}
    </p>
  );
}

// The words as on slide 7: captions 12 px muted grey, labels 14 px, values
// 14 px medium in the foreground. The cards are surfaces resting on the
// slide: no shadow.
export function WhereItRuns() {
  const [at, setAt] = useState(0);
  const [moving, setMoving] = useState(true);
  const [focus, setFocus] = useState<Part | null>(null);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches)
      setMoving(false);
  }, []);
  useEffect(() => {
    if (!moving) return;
    const timer = window.setTimeout(() => setAt((a) => (a + 1) % 2), HOLD);
    return () => window.clearTimeout(timer);
  }, [moving, at]);
  const go = (i: number) => {
    setAt((i + 2) % 2);
    setMoving(false);
  };
  const where = options[at].id;
  const own = where === "own";

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Cloud />}>{delivery.label}</Label>}
        heading={delivery.heading}
        lead={delivery.lead}
      />

      {/* The two options, as slide 6's people. */}
      <div
        role="radiogroup"
        aria-label="Where it runs"
        className="grid gap-4 md:grid-cols-2"
      >
        {options.map((o, i) => {
          const here = i === at;
          const name = delivery.options.find((d) => d.id === o.id)!.name;
          return (
            <button
              key={o.id}
              type="button"
              role="radio"
              aria-checked={here}
              {...mouseOnly}
              onClick={() => go(i)}
              className={cn(
                panel,
                "group relative flex cursor-pointer items-center gap-4 overflow-hidden px-4 py-4 text-left transition-[border-color,background-color] duration-normal ease-move hover:border-primary/40 focus-visible:ring-2 focus-visible:ring-ring",
                here && "border-primary/50 bg-primary/5",
              )}
            >
              <span
                className={cn(
                  "flex size-16 shrink-0 items-center justify-center rounded-2xl bg-background text-primary ring-1 transition-[box-shadow,translate] duration-normal ease-move group-hover:-translate-y-0.5 dark:bg-card [&_svg]:size-7",
                  here
                    ? "ring-2 ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                    : "ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] group-hover:ring-primary/40",
                )}
              >
                {o.icon}
              </span>
              <span className="grid min-w-0 gap-1">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="text-base font-semibold text-foreground">
                    {name}
                  </span>
                  <Badge variant="secondary" className="shrink-0">
                    {o.badge}
                  </Badge>
                </span>
                <span className="truncate text-sm text-muted-foreground">
                  {o.line}
                </span>
                <StatusBadge type="success" className="mt-1">
                  {o.status}
                </StatusBadge>
              </span>
              {here && moving && (
                <span
                  aria-hidden="true"
                  className="absolute inset-x-4 bottom-1.5 h-1 overflow-hidden rounded-full bg-primary/15"
                >
                  <span
                    key={at}
                    className="block h-full rounded-full bg-primary motion-safe:animate-[tour-fill-x_linear_forwards]"
                    style={{ animationDuration: `${HOLD}ms` }}
                  />
                </span>
              )}
            </button>
          );
        })}
      </div>

      <div className="grid gap-6 lg:grid-cols-[1.35fr_1fr]">
        <div className={cn(panel, "grid content-start gap-6 p-5")}>
          {/* What does not change: someone ships an app, the same either way. */}
          <div className="grid gap-3">
            <BoxHeading icon={<Rocket />}>
              For whoever builds, the same on both
            </BoxHeading>
            <div
              className={cn(
                "flex items-center gap-3 rounded-2xl px-1 transition-colors duration-normal",
              )}
            >
              <RouteBox lit={focus === "build"}>
                <PersonMark look="anyone" className="size-7" />
              </RouteBox>
              <span className="rounded-md bg-muted px-2 py-1 font-mono text-xs text-foreground">
                shpyrd ship expenses
              </span>
              <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm font-medium text-foreground">
                expenses.yourcompany.com
              </span>
              <StatusBadge type="success">Live</StatusBadge>
            </div>
          </div>

          {/* What changes: where things are. The dashed lines around the
              boxes, your company's on the left and ours on the right; in your
              own cloud, one around both. */}
          <div className="grid gap-3 border-t border-border pt-5">
            <BoxHeading icon={<MapIcon />}>
              Where it runs, and who looks after it
            </BoxHeading>
            <div className="relative mt-3 px-5 py-9">
              <div
                aria-hidden="true"
                className={cn(
                  "absolute inset-y-0 left-0 rounded-2xl border border-dashed border-info/60 transition-[width,background-color] duration-slow ease-move",
                  own ? "w-full bg-info/5" : "w-[calc(50%-1.25rem)]",
                )}
              />
              <div
                aria-hidden="true"
                className={cn(
                  "absolute inset-y-0 right-0 w-[calc(50%-1.25rem)] rounded-2xl border border-dashed border-primary/60 bg-primary/5 transition-opacity duration-slow ease-move",
                  own ? "opacity-0" : "opacity-100",
                )}
              />
              <p
                key={`left-${where}`}
                className="absolute -top-2.5 left-4 bg-card px-1.5 text-xs font-medium text-info animate-in fade-in duration-normal"
              >
                {own ? delivery.ownZone : delivery.company}
              </p>
              <p
                className={cn(
                  "absolute -top-2.5 right-4 bg-card px-1.5 text-xs font-medium text-primary transition-opacity duration-normal",
                  own && "opacity-0",
                )}
              >
                {delivery.options[0].name}
              </p>

              <div className="grid grid-cols-[1fr_4rem_1fr] items-center gap-x-3 gap-y-6">
                <Box icon={<KeyRound />} note="Entra · Google · Okta">
                  Your sign-in
                </Box>
                <Thread at={where} delay={200} />
                <Box
                  icon={<Waypoints />}
                  lit={focus === "platform"}
                  note={own ? "Run by your team" : "Run and updated by us"}
                >
                  {delivery.platformBox}
                </Box>
                <Box icon={<Server />} note="What your apps reach">
                  {delivery.systems}
                </Box>
                <Thread at={where} delay={500} />
                <Box
                  icon={<Database />}
                  lit={focus === "data"}
                  note="Your data"
                >
                  {delivery.databases}
                </Box>
              </div>
            </div>
            <p
              key={where}
              className="text-center text-xs text-muted-foreground animate-in fade-in duration-normal"
            >
              {own ? delivery.ownLink : delivery.link}
            </p>
          </div>
        </div>

        {/* What changes, as four questions answered for the option shown. */}
        <div className={cn(panel, "grid content-start gap-2 p-5")}>
          <BoxHeading icon={<ListChecks />}>
            What changes on {own ? "your own cloud" : "shpyrd cloud"}
          </BoxHeading>
          <div className="-mx-2 grid gap-1">
            {changes.map((c, i) => (
              <div
                key={c.question}
                onMouseEnter={() => setFocus(c.part ?? null)}
                onMouseLeave={() => setFocus(null)}
                className={cn(
                  "flex items-center gap-3 rounded-xl px-2 py-2.5 transition-colors duration-normal",
                  c.part && "hover:bg-primary/5",
                )}
              >
                <RouteBox size="size-11">{c.icon}</RouteBox>
                <div className="grid gap-0.5">
                  <span className="text-xs text-muted-foreground">
                    {c.question}
                  </span>
                  <span
                    key={`${where}-${c.question}`}
                    className={cn("text-sm font-medium text-foreground", rise)}
                    style={{ animationDelay: `${i * 90}ms` }}
                  >
                    {c.answer[where]}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* The slides' controls: the other option, pause or play, the next. */}
      <div className="-mt-2 flex items-center justify-center gap-2">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => go(at - 1)}
          aria-label="Previous option"
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => setMoving((m) => !m)}
          aria-label={moving ? "Pause" : "Play"}
        >
          {moving ? (
            <Pause className="fill-current" />
          ) : (
            <Play className="translate-x-px fill-current" />
          )}
        </Button>
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => go(at + 1)}
          aria-label="Next option"
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
