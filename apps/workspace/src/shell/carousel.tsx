"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";

// A row of cards that goes sideways: scrolled, swiped, or a card at a
// time with the arrows in its header, which turn off at either end. The
// edge where there is more fades. Owed to design/ui once it settles.
export function Carousel({ heading, action, children }: { heading: string; action?: React.ReactNode; children: React.ReactNode }) {
  const row = useRef<HTMLDivElement>(null);
  const [ends, setEnds] = useState({ start: true, end: true });
  const measure = () => {
    const el = row.current;
    if (!el) return;
    setEnds({ start: el.scrollLeft <= 1, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 1 });
  };
  useEffect(() => {
    measure();
    const el = row.current;
    if (!el) return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [children]);
  const step = (direction: 1 | -1) => {
    const el = row.current;
    const card = el?.firstElementChild as HTMLElement | null;
    el?.scrollBy({ left: direction * ((card?.offsetWidth ?? 200) + 12), behavior: "smooth" });
  };
  const fade = !ends.start && !ends.end ? "[mask-image:linear-gradient(to_right,transparent,black_2rem,black_calc(100%-2rem),transparent)]" : !ends.end ? "[mask-image:linear-gradient(to_right,black_calc(100%-3rem),transparent)]" : !ends.start ? "[mask-image:linear-gradient(to_right,transparent,black_3rem)]" : "";
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex items-center gap-3">
        <h2 className="font-heading text-base font-medium">{heading}</h2>
        <div className="ml-auto flex items-center gap-1">
          {action}
          {!(ends.start && ends.end) && (
            <>
              <Button variant="outline" size="icon-xs" icon={<ChevronLeft />} aria-label="Previous" disabled={ends.start} onClick={() => step(-1)} className="ml-2" />
              <Button variant="outline" size="icon-xs" icon={<ChevronRight />} aria-label="Next" disabled={ends.end} onClick={() => step(1)} />
            </>
          )}
        </div>
      </div>
      <div ref={row} role="list" aria-label={heading} onScroll={measure} className={`-m-1 flex snap-x snap-mandatory scroll-px-1 gap-3 overflow-x-auto p-1 [scrollbar-width:none] [&>*]:snap-start ${fade}`}>
        {children}
      </div>
    </section>
  );
}
