import { Card } from "@shpyrd/ui/components/card";

// A list of apps and who may open each: what a colleague sees when they sign
// in. Access is carried by the mark in the gutter. An app nobody has been
// given yet has no mark — being outside a team is an absence, not an error,
// so it is never shown as one.

export type Entry = { name: string; audience: string; shared: boolean };

export function Roster({
  caption,
  entries,
  className,
}: {
  caption: string;
  entries: Entry[];
  className?: string;
}) {
  return (
    <Card className={`overflow-hidden p-0 ${className ?? ""}`}>
      <p className="border-b px-4 py-3 text-sm text-muted-foreground">{caption}</p>
      <ul className="divide-y">
        {entries.map((entry) => (
          <li key={entry.name} className="flex gap-3 px-4 py-3">
            <span
              aria-hidden="true"
              className={`mt-1 w-0.5 shrink-0 self-stretch rounded-full ${
                entry.shared ? "bg-primary" : "bg-border"
              }`}
            />
            <span className="min-w-0">
              <span className="block font-medium">{entry.name}</span>
              <span
                className={`block text-sm text-muted-foreground ${
                  entry.shared ? "" : "italic"
                }`}
              >
                {entry.audience}
              </span>
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
