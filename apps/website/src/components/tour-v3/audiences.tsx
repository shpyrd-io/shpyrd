// Slide 10: who it is for. Four people, what each says, and what each gets.
import { Code2, HandCoins, Hammer, ShieldCheck, Users } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Checklist } from "@shpyrd/ui/components/checklist";
import { Tile } from "@shpyrd/ui/components/tile";
import { audiences } from "@/content/tour-v3";
import { Intro, Label, panel, rise } from "./parts";

const icons = [
  <Hammer key="h" />,
  <ShieldCheck key="s" />,
  <Code2 key="c" />,
  <HandCoins key="f" />,
];

export function Audiences() {
  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Users />}>{audiences.label}</Label>}
        heading={audiences.heading}
        lead={audiences.lead}
      />
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {audiences.groups.map((g, i) => (
          <Checklist
            key={g.name}
            as="h3"
            // As the home's cards: under the pointer only the edge turns orange.
            className={cn(
              panel,
              "transition-[box-shadow] duration-300 ease-out hover:ring-primary/50",
              rise,
            )}
            style={{ animationDelay: `${360 + i * 120}ms` }}
            heading={
              <span className="grid gap-2">
                <span className="flex items-center gap-2.5">
                  <Tile size="sm" variant="muted" className="text-primary">
                    {icons[i]}
                  </Tile>
                  {g.name}
                </span>
                <span className="text-sm font-normal text-muted-foreground italic">
                  {g.quote}
                </span>
              </span>
            }
            items={g.points}
          />
        ))}
      </div>
    </div>
  );
}
