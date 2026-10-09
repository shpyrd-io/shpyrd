"use client";

import * as React from "react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { AgentMark, useCurrentAgent } from "@/components/add-to-agent";
import { Composer } from "@/components/shipping-chat";

// A conversation with the reader's agent, in the window of the home page's
// chat (shipping-chat.tsx): the frame of glass, the window light with a
// gradient, the agent the "Add to" button names in its bar, and the box to
// type in at the bottom. The conversation is the page's own, as it was; when
// the reader reaches it, its messages arrive one after the other. Still, and
// whole, for who asked the system for less motion.
export function AgentWindow({
  detail,
  className,
  children,
  ...props
}: {
  detail?: string;
  className?: string;
  children: React.ReactNode;
} & React.ComponentProps<"div">) {
  const { agent } = useCurrentAgent();
  const ref = React.useRef<HTMLDivElement>(null);
  const [shown, setShown] = React.useState(false);

  React.useEffect(() => {
    const box = ref.current;
    if (!box) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setShown(true);
    const seen = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        setShown(true);
        seen.disconnect();
      },
      { threshold: 0.3 },
    );
    seen.observe(box);
    return () => seen.disconnect();
  }, []);

  return (
    <div ref={ref} {...props} className={cn(glass, "mx-auto w-full max-w-2xl rounded-[18px] p-2.5", className)}>
      <AppWindow
        title={
          <span className="inline-flex items-center gap-1.5">
            <AgentMark id={agent.id} className="size-3.5" />
            {agent.name}
          </span>
        }
        detail={detail}
        className={cn(
          "rounded-[10px] border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
          "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
        )}
        footer={<Composer text={null} placeholder={`Message ${agent.name}…`} />}
      >
        <div
          data-shown={shown || undefined}
          className={cn(
            "px-5 py-6",
            // Each message, then each step the agent runs, in turn.
            "[&_[data-slot=conversation]>*]:opacity-0 [&_[data-slot=conversation]>*]:translate-y-2 [&_[data-slot=conversation]>*]:transition-[opacity,translate] [&_[data-slot=conversation]>*]:duration-700 [&_[data-slot=conversation]>*]:ease-out",
            "data-shown:[&_[data-slot=conversation]>*]:opacity-100 data-shown:[&_[data-slot=conversation]>*]:translate-y-0",
            "[&_[data-slot=conversation]>*:nth-child(2)]:delay-[900ms] [&_[data-slot=conversation]>*:nth-child(3)]:delay-[1800ms] [&_[data-slot=conversation]>*:nth-child(4)]:delay-[2700ms] [&_[data-slot=conversation]>*:nth-child(5)]:delay-[3600ms] [&_[data-slot=conversation]>*:nth-child(6)]:delay-[4500ms]",
          )}
        >
          {children}
        </div>
      </AppWindow>
    </div>
  );
}
