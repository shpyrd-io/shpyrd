import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";

// A screen in a browser, in the look of the home hero's window (and the
// site's CodeWindow, AgentWindow): a frame of glass, 10px inside, round 18px;
// the window in it round 10px, its edge white, no shadow of its own, its bar
// on the window's ground.
export function Screen({ address, className, children }: { address: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={cn(glass, "rounded-[18px] p-2.5", className)}>
      <BrowserFrame
        address={address}
        className={cn(
          "h-full rounded-[10px] border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
          "[&>div:first-child]:border-foreground/6 [&>div:first-child]:bg-transparent",
        )}
      >
        {children}
      </BrowserFrame>
    </div>
  );
}

