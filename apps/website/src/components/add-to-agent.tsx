"use client";

import { useEffect, useRef, useState } from "react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { Button } from "@shpyrd/ui/components/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { addToAgent, agents } from "@shpyrd/content/site/agents";

// The developer action: connect a workspace to the agent you already use.
//
// The client name changes on its own, which is the page's one piece of motion
// nobody asked for — it carries the fact that this works with more than one
// agent rather than decorating. It stops while the panel is open, and entirely
// when the reader has asked for less motion.
const HOLD = 1600;

export function AddToAgent() {
  const [index, setIndex] = useState(0);
  const [open, setOpen] = useState(false);
  const [client, setClient] = useState(agents[0].id);
  // Rendered when the application is built, where there is no matchMedia; the
  // reader's preference is read in an effect instead.
  const [still, setStill] = useState(true);
  const paused = useRef(false);

  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)");
    const apply = () => setStill(less.matches);
    apply();
    less.addEventListener("change", apply);
    return () => less.removeEventListener("change", apply);
  }, []);

  useEffect(() => {
    if (still || open) return;
    const timer = window.setInterval(() => {
      if (!paused.current) setIndex((i) => (i + 1) % agents.length);
    }, HOLD);
    return () => window.clearInterval(timer);
  }, [still, open]);

  // Opening shows whichever client the reader was just looking at.
  function onOpenChange(next: boolean) {
    if (next) setClient(agents[index].id);
    setOpen(next);
  }

  return (
    <div
      onMouseEnter={() => (paused.current = true)}
      onMouseLeave={() => (paused.current = false)}
      onFocusCapture={() => (paused.current = true)}
      onBlurCapture={() => (paused.current = false)}
    >
      <AnchoredOverlay
        open={open}
        onOpenChange={onOpenChange}
        width="large"
        anchor={
          <Button aria-label={`${addToAgent.label} ${agents[index].name}`}>
            {addToAgent.label}
            {/* The names sit in one cell so the button keeps the width of the
                longest and the row never moves as they change. */}
            <span className="grid">
              {agents.map((agent, i) => (
                <span
                  key={agent.id}
                  aria-hidden={i === index ? undefined : "true"}
                  className="col-start-1 row-start-1 font-semibold transition-opacity duration-200"
                  style={{ opacity: i === index ? 1 : 0 }}
                >
                  {agent.name}
                </span>
              ))}
            </span>
          </Button>
        }
      >
        <Tabs value={client} onValueChange={setClient} className="min-w-0">
          {/* Five names do not fit on a narrow screen, so the list scrolls
              sideways rather than widening the overlay. The vertical axis is
              pinned: setting one axis to anything but visible makes the other
              compute to auto, which drew a scrollbar over a fixed-height row. */}
          <TabsList className="max-w-full overflow-x-auto overflow-y-hidden">
            {agents.map((agent) => (
              <TabsTrigger key={agent.id} value={agent.id}>
                {agent.name}
              </TabsTrigger>
            ))}
          </TabsList>
          {agents.map((agent) => (
            <TabsContent key={agent.id} value={agent.id} className="grid min-w-0 gap-2">
              {agent.file && <p className="text-xs text-muted-foreground">{agent.file}</p>}
              {/* The command wraps rather than scrolling: a scrollbar in a
                  panel this narrow hides half the line, and a reader has to
                  see the whole thing to copy it. break-all because a URL and
                  a path have nowhere else to break. */}
              <pre className="min-w-0 rounded-lg border bg-muted p-3 text-xs whitespace-pre-wrap break-all">
                <code>{agent.snippet}</code>
              </pre>
            </TabsContent>
          ))}
        </Tabs>
        <p className="mt-3 text-xs text-muted-foreground">{addToAgent.note}</p>
        <a
          href={addToAgent.docs.href}
          className="mt-2 inline-block text-xs underline underline-offset-4"
        >
          {addToAgent.docs.label}
        </a>
      </AnchoredOverlay>
    </div>
  );
}
