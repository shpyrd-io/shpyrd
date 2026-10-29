import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { X } from "lucide-react";
import { cn } from "../lib/cn";
import { Button } from "./button";

// A line of news across the top of a site, over its header: one sentence,
// and at most one link to go on, as neon.com has it. Its ground does not
// follow the theme of the page: it is black, white or orange, chosen. The
// black and the white take the tokens of their theme for themselves; on the
// orange, text and icons are white, as design/brand says.
//
// What lasts on a page and is about the page goes in an Alert instead.
const announcementBarVariants = cva(
  "@container/announcement-bar relative isolate overflow-hidden border-b before:pointer-events-none before:absolute before:inset-0 before:-z-10 after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-px after:bg-linear-to-r after:from-transparent after:to-transparent",
  {
    variants: {
      variant: {
        // A glow of the brand from under the middle of the bar, faint, and
        // a thread of orange along its foot, brightest in the middle.
        dark: "dark border-border bg-background text-foreground before:bg-[radial-gradient(40%_160%_at_50%_100%,color-mix(in_oklab,var(--color-primary)_28%,transparent),transparent)] after:via-primary/60",
        light: "light border-border bg-background text-foreground before:bg-[radial-gradient(40%_160%_at_50%_100%,color-mix(in_oklab,var(--color-primary)_12%,transparent),transparent)] after:via-primary/50",
        // On the orange the light is white: a lighter middle, and a white
        // thread.
        orange: "border-transparent bg-primary text-primary-foreground before:bg-[radial-gradient(40%_160%_at_50%_100%,color-mix(in_oklab,var(--color-primary-foreground)_22%,transparent),transparent)] after:via-primary-foreground/50",
      },
    },
    defaultVariants: {
      variant: "dark",
    },
  },
);

// What is said after the lead, and the link: quieter than the lead on
// black and white; on the orange all of it is white.
const quiet = {
  dark: "text-muted-foreground",
  light: "text-muted-foreground",
  orange: "text-primary-foreground/85",
} as const;

const link = {
  dark: "text-primary",
  light: "text-primary",
  orange: "text-primary-foreground [&_a]:underline",
} as const;

function AnnouncementBar({
  className,
  variant,
  icon,
  action,
  onDismiss,
  children,
  ...props
}: React.ComponentProps<"div"> &
  VariantProps<typeof announcementBarVariants> & {
    // Before the text: the mark, most often, on the same ground
    // (`<LogoMark surface="dark" />`, `"light"` or `"orange"`).
    icon?: React.ReactElement;
    // After the text: one link, with an arrow at its end when it goes on.
    action?: React.ReactNode;
    // With it, a button closes the bar.
    onDismiss?: () => void;
  }) {
  const ground = variant ?? "dark";
  return (
    <div
      data-slot="announcement-bar"
      data-variant={ground}
      role="region"
      aria-label="Announcement"
      className={cn(announcementBarVariants({ variant: ground }), className)}
      {...props}
    >
      <div
        className={cn(
          "mx-auto flex min-h-10 max-w-7xl flex-wrap items-center justify-center gap-x-2.5 gap-y-0.5 px-4 py-2 text-center text-xs/5 @xl/announcement-bar:text-sm/5",
          onDismiss && "pr-11",
        )}
      >
        {/* In a narrow room the icon would take a line of its own: the
            words go first, and it is left out. */}
        {icon && (
          <span
            data-slot="announcement-bar-icon"
            className="hidden shrink-0 @md/announcement-bar:inline-flex [&_svg]:size-4"
          >
            {icon}
          </span>
        )}
        <p
          data-slot="announcement-bar-text"
          className={cn("text-pretty [&_strong]:font-medium", quiet[ground], ground === "orange" ? "[&_strong]:text-primary-foreground" : "[&_strong]:text-foreground")}
        >
          {children}
        </p>
        {action && (
          <span
            data-slot="announcement-bar-action"
            className={cn(
              "inline-flex shrink-0 font-medium [&_a]:inline-flex [&_a]:items-center [&_a]:gap-1 [&_a]:underline-offset-4 [&_a]:transition-colors [&_a]:duration-fast [&_a:hover]:underline [&_svg]:size-3.5 [&_svg]:transition-transform [&_svg]:duration-fast [&_a:hover_svg]:translate-x-0.5",
              link[ground],
            )}
          >
            {action}
          </span>
        )}
      </div>
      {onDismiss && (
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Dismiss"
          onClick={onDismiss}
          className={cn(
            "absolute top-1/2 right-2 -translate-y-1/2",
            ground === "orange" ? "text-primary-foreground hover:bg-primary-foreground/15 hover:text-primary-foreground" : "text-muted-foreground",
          )}
        >
          <X />
        </Button>
      )}
    </div>
  );
}

export { AnnouncementBar, announcementBarVariants };
