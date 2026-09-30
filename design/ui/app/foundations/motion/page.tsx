"use client";

import { useState } from "react";
import { cn } from "cn";
import { Loader2, PanelRight, Play } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Section } from "../../section";

// How long a change takes, by what changes.
const durations = [
  {
    name: "duration-fast",
    value: "100ms",
    use: "Something answers the pointer: a hover, a press, a tooltip, a menu.",
    className: "duration-fast",
  },
  {
    name: "duration-normal",
    value: "200ms",
    use: "Something appears or leaves: a dialog, an overlay, a toast.",
    className: "duration-normal",
  },
  {
    name: "duration-slow",
    value: "400ms",
    use: "Something moves or changes its size: a pane, a bar that fills.",
    className: "duration-slow",
  },
];

// How a change speeds up and slows down, by where it is going. The four
// numbers are those of the curve, as `cubic-bezier` takes them.
const easings = [
  {
    name: "ease-enter",
    curve: [0, 0, 0.2, 1],
    use: "Something arrives: it comes quickly and settles.",
    className: "ease-enter",
  },
  {
    name: "ease-exit",
    curve: [0.4, 0, 1, 1],
    use: "Something leaves: it starts slowly and is gone.",
    className: "ease-exit",
  },
  {
    name: "ease-move",
    curve: [0.4, 0, 0.2, 1],
    use: "Something stays on the page and goes from one place to another.",
    className: "ease-move",
  },
];

export default function Page() {
  const [timed, setTimed] = useState(false);
  const [eased, setEased] = useState(false);
  const [shown, setShown] = useState(true);
  const [open, setOpen] = useState(false);
  return (
    <>
      <Section title="Durations">
        <Stack gap="normal" className="max-w-2xl">
          {durations.map((duration) => (
            <Row key={duration.name} name={duration.name} value={duration.value} use={duration.use}>
              <Track there={timed} className={cn("ease-move", duration.className)} />
            </Row>
          ))}
          <PlayButton onClick={() => setTimed(!timed)} />
        </Stack>
      </Section>
      <Section title="Easings, each over a second so that the curve is seen">
        <Stack gap="normal" className="max-w-2xl">
          {easings.map((easing) => (
            <Row
              key={easing.name}
              name={easing.name}
              value={`cubic-bezier(${easing.curve.join(", ")})`}
              use={easing.use}
              figure={<Curve points={easing.curve} />}
            >
              <Track there={eased} className={cn("duration-1000", easing.className)} />
            </Row>
          ))}
          <PlayButton onClick={() => setEased(!eased)} />
        </Stack>
      </Section>
      <Section title="Something appears and leaves: normal to enter, fast to exit">
        <Stack gap="cozy" align="start">
          <Button variant="outline" size="sm" onClick={() => setShown(!shown)}>
            {shown ? "Hide" : "Show"}
          </Button>
          <Card
            aria-hidden={!shown}
            className={cn(
              "w-full max-w-sm origin-top-left transition-[opacity,scale]",
              shown
                ? "scale-100 opacity-100 duration-normal ease-enter"
                : "scale-95 opacity-0 duration-fast ease-exit",
            )}
          >
            <CardHeader>
              <CardTitle>Backups</CardTitle>
              <CardDescription>The last one was taken four hours ago.</CardDescription>
            </CardHeader>
          </Card>
        </Stack>
      </Section>
      <Section title="Something changes its size: slow, and the curve of what moves">
        <Stack gap="cozy" align="start">
          <Button variant="outline" size="sm" icon={<PanelRight />} onClick={() => setOpen(!open)}>
            {open ? "Close the pane" : "Open the pane"}
          </Button>
          <div className="flex h-40 w-full max-w-2xl overflow-hidden rounded-lg ring-1 ring-foreground/10">
            <div className="grid min-w-0 flex-1 content-start gap-2 p-4">
              <Skeleton className="h-4 w-2/3 animate-none" />
              <Skeleton className="h-4 w-full animate-none" />
              <Skeleton className="h-4 w-1/2 animate-none" />
            </div>
            <div
              className={cn(
                "shrink-0 overflow-hidden bg-muted transition-[width] duration-slow ease-move",
                open ? "w-56" : "w-0",
              )}
            >
              <p className="w-56 p-4 text-sm text-muted-foreground">What is known of the row that was chosen.</p>
            </div>
          </div>
        </Stack>
      </Section>
      <Section title="Without an end: something is still going on">
        <Stack gap="cozy" className="max-w-2xl">
          <Row name="animate-spin" use="Working: the answer is on its way.">
            <Loader2 aria-label="Working" className="size-5 animate-spin text-muted-foreground" />
          </Row>
          <Row name="animate-pulse" use="The shape of what is coming, and the dot of what is live.">
            <Stack direction="horizontal" align="center" gap="normal">
              <Skeleton className="h-4 w-40" />
              <StatusBadge type="success" live>
                Running
              </StatusBadge>
            </Stack>
          </Row>
        </Stack>
      </Section>
    </>
  );
}

// A name of the library, its value, what it is for, and what shows it.
function Row({
  name,
  value,
  use,
  figure,
  children,
}: {
  name: string;
  value?: string;
  use: string;
  figure?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Stack direction="horizontal" align="center" gap="normal">
      {figure}
      <Stack gap="condensed" className="min-w-0 flex-1">
        <Stack direction="horizontal" wrap="wrap" align="baseline" gap="cozy">
          <span className="font-mono text-xs">{name}</span>
          {value && <span className="font-mono text-xs text-muted-foreground">{value}</span>}
          <span className="text-sm text-muted-foreground">{use}</span>
        </Stack>
        {children}
      </Stack>
    </Stack>
  );
}

// A dot that goes from one end to the other, the way its classes say.
function Track({ there, className }: { there: boolean; className: string }) {
  return (
    <div className="relative h-6 rounded-full bg-muted">
      <div
        className={cn(
          "absolute top-1 size-4 rounded-full bg-primary transition-[left]",
          there ? "left-[calc(100%-1.25rem)]" : "left-1",
          className,
        )}
      />
    </div>
  );
}

// The curve of an easing: the time goes to the right, the change goes up.
function Curve({ points: [x1, y1, x2, y2] }: { points: number[] }) {
  const x = (n: number) => 4 + 40 * n;
  const y = (n: number) => 44 - 40 * n;
  return (
    <svg viewBox="0 0 48 48" aria-hidden className="size-12 shrink-0 rounded-md bg-muted">
      <path
        d={`M ${x(0)} ${y(0)} C ${x(x1)} ${y(y1)}, ${x(x2)} ${y(y2)}, ${x(1)} ${y(1)}`}
        fill="none"
        strokeWidth="2"
        strokeLinecap="round"
        className="stroke-primary"
      />
    </svg>
  );
}

function PlayButton({ onClick }: { onClick: () => void }) {
  return (
    <div>
      <Button variant="outline" size="sm" icon={<Play />} onClick={onClick}>
        Play
      </Button>
    </div>
  );
}
