"use client";

// The background of the subpages' heroes (Giovani, 2026-11-13 to 16: the
// shpyrd mark that was there, in the manifesto's style, in place of the
// binary ship's rain): the mark made solid in binary characters, large, at
// the right, its middle on the hero's middle, turning toward the pointer
// (tour-v3/icon-prism.tsx), its digits a quarter larger than the
// manifesto's. It turns only while it is in view.
import { useEffect, useRef, useState } from "react";
import { IconPrism, type IconName, type ShapeName } from "@/components/tour-v3/icon-prism";

export function HeroPrism({ icon, grain = 1.25 }: { icon: IconName | ShapeName; grain?: number }) {
  const box = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(true);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const watch = new IntersectionObserver(([e]) => setSeen(e.isIntersecting));
    watch.observe(el);
    // Its middle on the hero's middle, top to bottom, following the hero's
    // height: the hero is in the same ground as it (landing.tsx).
    const ground = el.parentElement;
    const hero = ground?.parentElement?.querySelector<HTMLElement>("[data-slot=hero]");
    let resized: ResizeObserver | undefined;
    if (ground && hero) {
      const place = () => {
        const g = ground.getBoundingClientRect();
        const h = hero.getBoundingClientRect();
        el.style.top = `${h.top - g.top + h.height / 2 - el.offsetHeight / 2}px`;
      };
      place();
      resized = new ResizeObserver(place);
      resized.observe(hero);
    }
    return () => {
      watch.disconnect();
      resized?.disconnect();
    };
  }, []);
  return (
    <div aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-0 -z-10 h-[56rem] overflow-x-clip">
      <div ref={box} data-prism="" className="absolute top-[-0.5rem] left-[calc(50%+46px)] size-[50.6rem]">
        <IconPrism icon={icon} active={seen} grain={grain} className="size-full" />
      </div>
    </div>
  );
}
