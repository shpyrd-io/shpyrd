import { Anchor, Atom, Building2, Cpu, Flame, Globe, Hexagon, Leaf, Rocket, Zap } from "lucide-react";

// Companies that do not exist, as a mark and a name, to stand in for the
// customer logos a real page would have.
const names = [
  [Hexagon, "Acme"],
  [Globe, "Northwind"],
  [Atom, "Globex"],
  [Cpu, "Initech"],
  [Leaf, "Umbrella"],
  [Zap, "Hooli"],
  [Flame, "Stark"],
  [Anchor, "Wayne"],
  [Rocket, "Vandelay"],
  [Building2, "Soylent"],
] as const;

export const fictionalLogos = names.map(([Icon, name]) => (
  <span key={name} className="inline-flex items-center gap-2 font-heading text-lg font-semibold tracking-tight">
    <Icon aria-hidden={true} className="size-6" />
    {name}
  </span>
));
