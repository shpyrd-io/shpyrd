import * as React from "react";
import { ArrowUpRight } from "lucide-react";
import { cn } from "cn";
import { glass } from "../lib/glass";

// A customer story in short: who they are, what happened, who says so, and
// where to read all of it. The logo sits centred in a fixed-height area so
// logos of different shapes start the story at the same line.
//
// Only the link at the foot goes anywhere; the card itself is not a link.
// Cards in a row are as tall as the tallest, and their links line up.
function UseCaseCard({
  className,
  logo,
  story,
  name,
  role,
  company,
  link,
  ...props
}: Omit<React.ComponentProps<"div">, "children"> & {
  // The customer's logo: an image, an icon with a wordmark, or plain text.
  logo: React.ReactNode;
  story: React.ReactNode;
  // The person who tells it, shown as "Name, Role at Company".
  name?: string;
  role?: string;
  company?: string;
  link?: { href: string; label?: string };
}) {
  const person = [name, role && company ? `${role} at ${company}` : (role ?? company)]
    .filter(Boolean)
    .join(", ");

  return (
    <div
      data-slot="use-case-card"
      className={cn(glass, "flex h-full flex-col overflow-hidden", className)}
      {...props}
    >
      <div
        data-slot="use-case-card-logo"
        className="flex h-28 shrink-0 items-center justify-center px-6 text-foreground [&_img]:max-h-10 [&_svg]:shrink-0"
      >
        {logo}
      </div>

      <div className="flex flex-1 flex-col border-t border-foreground/8 dark:border-foreground/15">
        <div className="flex flex-1 flex-col gap-4 p-6">
          <p data-slot="use-case-card-story" className="text-base text-muted-foreground">
            {story}
          </p>
          {person && (
            <p data-slot="use-case-card-person" className="mt-auto text-base text-muted-foreground">
              {person}
            </p>
          )}
        </div>

        {link && (
          <div
            data-slot="use-case-card-link"
            className="border-t border-foreground/8 dark:border-foreground/15"
          >
            <a
              href={link.href}
              className="flex items-center justify-between gap-2 px-6 py-4 text-base font-medium text-primary outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset"
            >
              {link.label ?? "Read customer story"}
              <ArrowUpRight aria-hidden="true" className="size-4 shrink-0" />
            </a>
          </div>
        )}
      </div>
    </div>
  );
}

export { UseCaseCard };
