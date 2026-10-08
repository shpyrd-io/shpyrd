import * as React from "react";
import { Check } from "lucide-react";
import { cn } from "cn";
import { glass } from "../lib/glass";

// What a feature gives you, one thing to a line, each marked with an orange
// check. It is the list alone, for use in a card of your own or on the page.
function ChecklistItems({
  className,
  items,
  ...props
}: Omit<React.ComponentProps<"ul">, "children"> & {
  items: React.ReactNode[];
}) {
  return (
    <ul data-slot="checklist-items" className={cn("grid gap-3", className)} {...props}>
      {items.map((item, i) => (
        <li key={i} className="flex items-start gap-2.5 text-muted-foreground">
          <Check aria-hidden={true} className="mt-0.5 size-4 shrink-0 text-primary" strokeWidth={2.5} />
          <span>{item}</span>
        </li>
      ))}
    </ul>
  );
}

// A feature card with its checks: a heading, what it gives you, and a way to
// learn more at the foot. A picture can sit at its right, or under the text
// on a narrow card, running to the card's edge rather than held inside its
// padding, like the cards of Supabase's feature pages.
//
// Its parts are props rather than children it looks for, so a page rendered
// when the application is built draws the same thing as the browser does.
function Checklist({
  className,
  as: Heading = "h3",
  heading,
  items,
  action,
  picture,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // A section's own heading is `h2`, so the cards under it are `h3`.
  as?: "h3" | "h4";
  heading: React.ReactNode;
  items: React.ReactNode[];
  // At the bottom left: usually an outline button, "Learn more".
  action?: React.ReactNode;
  // At the right of a wide card and under the text of a narrow one, bleeding
  // to the edge. What is drawn there is the picture's own to size and crop.
  picture?: React.ReactNode;
}) {
  return (
    <div
      data-slot="checklist"
      className={cn(
        "@container/checklist overflow-hidden",
        glass,
        className,
      )}
      {...props}
    >
      <div
        className={cn(
          "grid h-full",
          picture && "@xl/checklist:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]",
        )}
      >
        <div className="flex flex-col items-start gap-5 p-6 @xl/checklist:p-8">
          <Heading data-slot="checklist-heading" className="font-heading text-xl font-semibold">
            {heading}
          </Heading>
          <ChecklistItems items={items} />
          {action && (
            <div data-slot="checklist-action" className="mt-auto pt-3">
              {action}
            </div>
          )}
        </div>

        {picture && (
          <div
            data-slot="checklist-picture"
            className="relative min-h-40 min-w-0 self-end overflow-hidden pl-6 @xl/checklist:h-full @xl/checklist:self-stretch @xl/checklist:pl-0 @xl/checklist:pt-8"
          >
            {picture}
          </div>
        )}
      </div>
    </div>
  );
}

export { Checklist, ChecklistItems };
