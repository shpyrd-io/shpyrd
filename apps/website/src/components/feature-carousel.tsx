"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";

// A proposal for the home's features (Giovani, 2026-10-19, after the
// "Otimize o desempenho dos pagamentos" section of stripe.com/br/payments):
// the heading centred as the home's are, two arrows on the right, and under
// them the feature blocks as a row of cards that scrolls sideways, one card at a time.
// The cards themselves are FeatureBlocks', laid out for a carousel.
export function FeatureCarousel({
  heading,
  description,
  children,
}: {
  heading: React.ReactNode;
  description?: React.ReactNode;
  children: React.ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [ends, setEnds] = useState({ start: true, end: false });
  // The margins outside the content, where the cards fade and blur.
  const [sides, setSides] = useState({ left: 0, right: 0 });

  const track = () => box.current?.querySelector<HTMLElement>(".fb") ?? null;

  useEffect(() => {
    const el = track();
    const section = box.current;
    if (!el || !section) return;
    // The row runs out to the window's edges, past the page's content, and
    // fades only out there: within the content every card shows whole; the
    // ones going out, or coming in, fade over the margin beside it.
    const lay = () => {
      const page = document.documentElement.clientWidth;
      const r = section.getBoundingClientRect();
      const left = Math.max(r.left, 0);
      const right = Math.max(page - r.right, 0);
      Object.assign(el.style, {
        marginLeft: `${-left}px`,
        marginRight: `${-right}px`,
        paddingLeft: `${left}px`,
        paddingRight: `${right}px`,
        scrollPaddingLeft: `${left}px`,
      });
      setSides({ left, right });
      const mask = `linear-gradient(to right, transparent 0, black ${left}px, black calc(100% - ${right}px), transparent 100%)`;
      el.style.maskImage = mask;
      el.style.webkitMaskImage = mask;
    };
    const read = () =>
      setEnds({ start: el.scrollLeft <= 4, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 4 });
    lay();
    read();
    el.addEventListener("scroll", read, { passive: true });

    // Dragged with the mouse, as on stripe.com: the row follows the pointer,
    // without snapping while held, and settles on a card when let go. A drag
    // is not a click on what it started on. Touch scrolls by itself.
    let from: { x: number; left: number } | null = null;
    let moved = false;
    el.style.cursor = "grab";
    const down = (e: PointerEvent) => {
      if (e.pointerType !== "mouse" || e.button !== 0) return;
      from = { x: e.clientX, left: el.scrollLeft };
      moved = false;
    };
    const move = (e: PointerEvent) => {
      if (!from) return;
      const dx = e.clientX - from.x;
      if (!moved && Math.abs(dx) < 4) return;
      if (!moved) {
        moved = true;
        el.setPointerCapture(e.pointerId);
        el.style.scrollSnapType = "none";
        el.style.cursor = "grabbing";
        el.style.userSelect = "none";
      }
      el.scrollLeft = from.left - dx;
    };
    const up = () => {
      if (!from) return;
      from = null;
      el.style.cursor = "grab";
      el.style.userSelect = "";
      if (!moved) return;
      // Settle on the nearest card, then let the snapping take over again.
      const card = el.firstElementChild as HTMLElement | null;
      const step = card ? card.offsetWidth + 20 : 0;
      if (step) el.scrollTo({ left: Math.round(el.scrollLeft / step) * step, behavior: "smooth" });
      window.setTimeout(() => (el.style.scrollSnapType = ""), 400);
    };
    const click = (e: MouseEvent) => {
      if (moved) {
        e.preventDefault();
        e.stopPropagation();
        moved = false;
      }
    };
    const noDrag = (e: DragEvent) => e.preventDefault();
    el.addEventListener("pointerdown", down);
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
    el.addEventListener("click", click, true);
    el.addEventListener("dragstart", noDrag);
    const seen = new ResizeObserver(() => {
      lay();
      read();
    });
    seen.observe(section);
    window.addEventListener("resize", lay);
    return () => {
      el.removeEventListener("pointerdown", down);
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      el.removeEventListener("click", click, true);
      el.removeEventListener("dragstart", noDrag);
      el.removeEventListener("scroll", read);
      window.removeEventListener("resize", lay);
      seen.disconnect();
    };
  }, []);

  // One card further, the width of a card and the gap after it.
  const move = (way: 1 | -1) => {
    const el = track();
    const card = el?.firstElementChild as HTMLElement | null;
    if (!el || !card) return;
    el.scrollBy({ left: way * (card.offsetWidth + 20), behavior: "smooth" });
  };

  const arrow =
    "grid size-9 place-items-center rounded-full bg-muted text-foreground transition-colors hover:bg-primary hover:text-white disabled:pointer-events-none disabled:opacity-35 [&_svg]:size-4";

  return (
    <section ref={box} className="grid gap-6">
      {/* The heading centred, as the home's sections are; the arrows over the
          row's right end. */}
      <SectionIntro
        variant="xlarge"
        align="center"
        heading={heading}
        description={description}
        className="[&_[data-slot=section-intro-description]]:max-w-none [&_[data-slot=section-intro-heading]]:max-w-none"
      />
      <div className="mt-6 flex justify-end gap-2">
        <button type="button" aria-label="Previous" className={arrow} disabled={ends.start} onClick={() => move(-1)}>
          <ArrowLeft />
        </button>
        <button type="button" aria-label="Next" className={arrow} disabled={ends.end} onClick={() => move(1)}>
          <ArrowRight />
        </button>
      </div>
      {/* min-w-0: a grid item, it must not grow to the row of cards inside it. */}
      <div className="relative min-w-0">
        {children}
        {/* Over each margin, a blur that grows towards the window's edge, as
            the cards there fade: a frosted edge rather than a cut. */}
        {(["left", "right"] as const).map((side) => (
          <div
            key={side}
            aria-hidden="true"
            className="pointer-events-none absolute inset-y-0 backdrop-blur-[6px]"
            style={{
              [side]: -sides[side],
              width: sides[side],
              maskImage: `linear-gradient(to ${side}, transparent, black)`,
              WebkitMaskImage: `linear-gradient(to ${side}, transparent, black)`,
            }}
          />
        ))}
      </div>
    </section>
  );
}
