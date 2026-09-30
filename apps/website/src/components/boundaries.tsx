import type { Boundary } from "@shpyrd/content/site/boundaries";

// The limits, typeset as content rather than hidden in a footnote. A claim and
// the thing it does not cover are read together, which is the whole point.
export function Boundaries({ items }: { items: Boundary[] }) {
  return (
    <ul className="grid gap-6">
      {items.map((item) => (
        <li key={item.id} className="border-l-2 pl-4">
          <p className="font-semibold">{item.claim}</p>
          <p className="mt-1 max-w-prose text-muted-foreground">{item.limit}</p>
        </li>
      ))}
    </ul>
  );
}
