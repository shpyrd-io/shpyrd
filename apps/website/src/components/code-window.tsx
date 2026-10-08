import { SquareTerminal } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { IDE } from "@shpyrd/ui/components/ide";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";

// A block of code (or a terminal's output) in the window of the home page's
// chat: one window, in its frame of glass, the window light with a gradient,
// its bar naming what it is. The code is the library's IDE as it was, with
// its colours and its copy button, only without a frame of its own.
export function CodeWindow({
  title,
  detail,
  code,
  language,
  icon = <SquareTerminal />,
  className,
  roomy = false,
}: {
  title: string;
  detail?: string;
  code: string;
  language: string;
  // The mark beside the title: a terminal, or a file for a file.
  icon?: React.ReactElement;
  className?: string;
  // Room above and under the code: for a window that stands beside a hero's
  // words and should be about as tall as they are.
  roomy?: boolean;
}) {
  return (
    <div className={cn(glass, "rounded-[18px] p-2.5", className)}>
      <AppWindow
        title={
          <span className="inline-flex items-center gap-1.5 [&_svg]:size-3.5 [&_svg]:text-primary">
            {icon}
            {title}
          </span>
        }
        detail={detail}
        className={cn(
          "rounded-[10px] border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
          "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent",
        )}
      >
        <IDE
          code={code}
          language={language}
          showLineNumbers={false}
          className={cn("rounded-none bg-transparent px-1 py-2 ring-0", roomy && "py-10")}
        />
      </AppWindow>
    </div>
  );
}
