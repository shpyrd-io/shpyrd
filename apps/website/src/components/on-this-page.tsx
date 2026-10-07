"use client";

import * as React from "react";
import { ArrowUp } from "lucide-react";
import { NavList, NavListItem } from "@shpyrd/ui/components/nav-list";

type Part = { id: string; title: string; level: number };

// The parts of a text, beside it, the one being read lit as the page scrolls:
// after the table of contents of legal.shpyrd.io. A part is the one being read
// from when its heading passes 80px under the top of the window until the next
// one does. When the list is taller than its room, the lit part is kept in view.
export function OnThisPage({ parts }: { parts: Part[] }) {
  const [here, setHere] = React.useState("");
  const list = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const seen = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) if (entry.isIntersecting) setHere(entry.target.id);
      },
      { rootMargin: "-80px 0px -65% 0px" },
    );
    for (const part of parts) {
      const el = document.getElementById(part.id);
      if (el) seen.observe(el);
    }
    return () => seen.disconnect();
  }, [parts]);

  // Keep the lit part in view inside the pane, when the pane scrolls. Only the
  // pane moves: scrolling the item into view would move the page too, and stop
  // a glide under way.
  React.useEffect(() => {
    const lit = list.current?.querySelector<HTMLElement>('[aria-current="location"]');
    let pane = list.current?.parentElement ?? null;
    while (pane && !/(auto|scroll)/.test(getComputedStyle(pane).overflowY)) pane = pane.parentElement;
    if (!lit || !pane || pane.scrollHeight <= pane.clientHeight) return;
    const box = pane.getBoundingClientRect();
    const item = lit.getBoundingClientRect();
    if (item.top < box.top) pane.scrollTop -= box.top - item.top + 8;
    else if (item.bottom > box.bottom) pane.scrollTop += item.bottom - box.bottom + 8;
  }, [here]);

  // Glide to the part rather than jump, as on legal.shpyrd.io; the address
  // still names it, so it can be shared.
  const go = (event: React.MouseEvent, id: string) => {
    const target = document.getElementById(id);
    if (!target) return;
    event.preventDefault();
    setHere(id);
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    target.scrollIntoView({ behavior: still ? "auto" : "smooth", block: "start" });
    history.replaceState(null, "", `#${id}`);
  };

  return (
    <div ref={list}>
      <NavList aria-label="On this page" heading="On this page" headingLevel="h2">
        {parts.map((h) => (
          <NavListItem
            key={h.id}
            asChild
            aria-current={here === h.id ? "location" : undefined}
            className={h.level === 3 ? "pl-5" : undefined}
          >
            <a href={`#${h.id}`} onClick={(event) => go(event, h.id)}>
              {h.title}
            </a>
          </NavListItem>
        ))}
      </NavList>
      <a
        href="#top"
        onClick={(event) => {
          event.preventDefault();
          window.scrollTo({ top: 0, behavior: "smooth" });
        }}
        className="mx-4 mt-5 inline-flex items-center gap-2 text-xs text-muted-foreground transition-colors hover:text-primary"
      >
        <ArrowUp className="size-3" aria-hidden="true" />
        Back to top
      </a>
    </div>
  );
}
