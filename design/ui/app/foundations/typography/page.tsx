import { Section } from "../../section";

const sizes = [
  ["text-2xl", "text-2xl"],
  ["text-xl", "text-xl"],
  ["text-lg", "text-lg"],
  ["text-base", "text-base"],
  ["text-sm", "text-sm"],
  ["text-xs", "text-xs"],
];

const weights = [
  ["font-normal", "font-normal"],
  ["font-medium", "font-medium"],
  ["font-semibold", "font-semibold"],
];

export default function Page() {
  return (
    <>
      <Section title="Fonts">
        <p className="font-sans text-lg">Geist, for everything that is read.</p>
        <p className="font-mono text-lg">Monospace, for names and addresses.</p>
      </Section>
      <Section title="Sizes">
        {sizes.map(([name, size]) => (
          <p key={name} className="flex items-baseline gap-4">
            <span className="w-24 shrink-0 font-mono text-xs text-muted-foreground">{name}</span>
            <span className={size}>A workspace takes its plan at birth</span>
          </p>
        ))}
      </Section>
      <Section title="Weights">
        {weights.map(([name, weight]) => (
          <p key={name} className="flex items-baseline gap-4">
            <span className="w-24 shrink-0 font-mono text-xs text-muted-foreground">{name}</span>
            <span className={weight}>A workspace takes its plan at birth</span>
          </p>
        ))}
      </Section>
      <Section title="Colours of text">
        <p>The text itself.</p>
        <p className="text-muted-foreground">What explains it, in grey.</p>
        <p className="text-primary">What leads somewhere.</p>
        <p className="text-destructive">What went wrong.</p>
      </Section>
    </>
  );
}
