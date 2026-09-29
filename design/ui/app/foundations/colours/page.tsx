import { Section } from "../../section";

const surfaces = [
  ["background", "bg-background"],
  ["card", "bg-card"],
  ["popover", "bg-popover"],
  ["muted", "bg-muted"],
  ["accent", "bg-accent"],
  ["secondary", "bg-secondary"],
];

const text = [
  ["foreground", "bg-foreground"],
  ["muted-foreground", "bg-muted-foreground"],
  ["primary-foreground", "bg-primary-foreground"],
];

const accents = [
  ["primary", "bg-primary"],
  ["destructive", "bg-destructive"],
  ["ring", "bg-ring"],
];

const lines = [
  ["border", "bg-border"],
  ["input", "bg-input"],
];

// How something is. An error is `destructive`.
const status = [
  ["success", "bg-success"],
  ["info", "bg-info"],
  ["warning", "bg-warning"],
  ["destructive", "bg-destructive"],
];

// What is written over each, when it is solid.
const over = [
  ["success-foreground", "bg-success-foreground"],
  ["info-foreground", "bg-info-foreground"],
  ["warning-foreground", "bg-warning-foreground"],
  ["destructive-foreground", "bg-destructive-foreground"],
];

// The inks of charts: four for series that mean nothing, in their order,
// one for "the others", and four for series that mean something.
const series = [
  ["chart-1", "bg-chart-1"],
  ["chart-2", "bg-chart-2"],
  ["chart-3", "bg-chart-3"],
  ["chart-4", "bg-chart-4"],
  ["chart-5", "bg-chart-5"],
];

const meaning = [
  ["chart-success", "bg-chart-success"],
  ["chart-info", "bg-chart-info"],
  ["chart-warning", "bg-chart-warning"],
  ["chart-error", "bg-chart-error"],
];

export default function Page() {
  return (
    <>
      <Swatches title="Surfaces" colours={surfaces} />
      <Swatches title="Text" colours={text} />
      <Swatches title="Accents" colours={accents} />
      <Swatches title="Lines" colours={lines} />
      <Swatches title="Status" colours={status} />
      <Swatches title="Over a status" colours={over} />
      <Swatches title="Charts: series" colours={series} />
      <Swatches title="Charts: series that mean something" colours={meaning} />
    </>
  );
}

function Swatches({ title, colours }: { title: string; colours: string[][] }) {
  return (
    <Section title={title}>
      <div className="grid grid-cols-2 gap-4 @3xl/page-layout:grid-cols-4">
        {colours.map(([name, colour]) => (
          <div key={name} className="grid gap-2">
            <div className={`h-14 rounded-lg ring-1 ring-foreground/10 ${colour}`} />
            <span className="font-mono text-xs text-muted-foreground">{name}</span>
          </div>
        ))}
      </div>
    </Section>
  );
}
