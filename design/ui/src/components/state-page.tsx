import * as React from "react";
import { cn } from "cn";
import { Loader2 } from "lucide-react";
import { LogoMark, Wordmark } from "./brand";

// A whole page that says one thing, in the plain style: the mark or an
// icon over the words, the words in the middle, the wordmark in the
// corner. It is what a person sees where no application answers, where
// one is not open to them, while one wakes up; with no words at all it is
// the mark alone, where there is nothing to say.
function StatePage({
  className,
  icon,
  picture,
  title,
  description,
  action,
  waiting = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  // Over the title: the mark, or an icon that says what happened.
  icon?: React.ReactElement;
  // Over the title, bigger than an icon and in no box: a drawing of what
  // is going on. It takes the place of the icon.
  picture?: React.ReactNode;
  title?: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  // Something is on its way: a spinner turns under the words.
  waiting?: boolean;
}) {
  return (
    <div
      data-slot="state-page"
      className={cn("relative flex min-h-svh items-center justify-center overflow-hidden bg-background text-foreground", className)}
      {...props}
    >
      <div className="grid justify-items-center gap-5 px-6 text-center">
        {picture ? (
          picture
        ) : icon ? (
          <span className="inline-flex size-16 items-center justify-center rounded-2xl bg-muted text-muted-foreground [&_svg]:size-8">
            {icon}
          </span>
        ) : (
          <LogoMark className="size-16" />
        )}
        {title && <h1 className="font-heading text-2xl font-semibold">{title}</h1>}
        {description && <p className="max-w-md text-sm text-balance text-muted-foreground">{description}</p>}
        {waiting && <Loader2 aria-label="Waiting" className="size-5 animate-spin text-muted-foreground" />}
        {action && <div className="flex flex-wrap items-center justify-center gap-2">{action}</div>}
        {children}
      </div>
      <Wordmark className="absolute bottom-5 left-5 h-5 opacity-60" />
    </div>
  );
}

export { StatePage };
