"use client";

import { useEffect, useRef, useState } from "react";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { cn } from "@shpyrd/ui/lib/cn";

// The shipyard seen through the bar of the logo's "</>" (Giovani,
// 2026-10-20), as in the YouTube vignettes (Video assets/Vinhetas YouTube,
// Tema-Estaleiro-A): a slanted bar, white, the yard at work inside it, its
// lines on white rather than multiplied into the orange. Once on the screen
// the bar rises from its foot along its slant; for who asked for less
// motion it is simply there.
//
// The bar in the square of the picture, wide enough to show nearly all the
// yard (Giovani, 2026-10-20), running past the site's right edge: drawn on
// a box 1.4 times the square's width, its foot from 0 to 0.96 of the square,
// its top from 0.38 to 1.34 (in % of the box below), leaning as the logo's
// bar does.
const shown = "polygon(0% 100%, 68.6% 100%, 95.7% 0%, 27.1% 0%)";
const hidden = "polygon(0% 100%, 68.6% 100%, 68.6% 100%, 0% 100%)";

export function ShipyardBar({ className }: { className?: string }) {
  const root = useRef<HTMLDivElement>(null);
  const yard = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  // The yard in parallax (Giovani, 2026-10-20): as the page scrolls, it
  // moves slower than the bar it is seen through, up to 110px
  // either way, as if further off. Only while the bar is on the screen, one
  // frame at a time, a transform only; not for who asked for less motion.
  useEffect(() => {
    const el = root.current;
    const inner = yard.current;
    if (!el || !inner || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    let frame = 0;
    let seen = false;
    const place = () => {
      frame = 0;
      const r = el.getBoundingClientRect();
      // -1 with the bar's middle at the foot of the window, 1 at its top.
      const at = Math.max(-1, Math.min(1, 1 - (2 * (r.top + r.height / 2)) / window.innerHeight));
      inner.style.translate = `0 ${(at * 110).toFixed(1)}px`;
    };
    const ask = () => {
      if (seen && !frame) frame = requestAnimationFrame(place);
    };
    const sight = new IntersectionObserver(([e]) => {
      seen = e.isIntersecting;
      ask();
    });
    sight.observe(el);
    window.addEventListener("scroll", ask, { passive: true });
    window.addEventListener("resize", ask);
    return () => {
      sight.disconnect();
      window.removeEventListener("scroll", ask);
      window.removeEventListener("resize", ask);
      cancelAnimationFrame(frame);
    };
  }, []);

  useEffect(() => {
    const el = root.current;
    if (!el) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return setOpen(true);
    const sight = new IntersectionObserver(([e]) => {
      if (e.isIntersecting) {
        setOpen(true);
        sight.disconnect();
      }
    }, { threshold: 0.3 });
    sight.observe(el);
    return () => sight.disconnect();
  }, []);

  return (
    // A soft shadow under the whole bar, cast on the orange (a drop shadow,
    // as the bar is cut out by its clip).
    <div ref={root} className={cn("relative aspect-square w-full drop-shadow-[0_18px_36px_rgb(80_20_0/0.28)]", className)}>
      {/* The bar is white in both themes (Giovani, 2026-10-20), so the yard
          in it is always the light one: `.light` for the theme's colours and
          `shipyard-light` for the drawings it would swap for their dark
          twins (src/styles/global.css). */}
      <div
        className="light shipyard-light absolute inset-y-0 -left-[50px] w-[140%] bg-white transition-[clip-path] duration-[1400ms] ease-[cubic-bezier(0.22,1,0.36,1)]"
        style={{ clipPath: open ? shown : hidden }}
      >
        {/* The whole yard, at the square's size, in the bar, 100px to the
            right of the bar's box (Giovani, 2026-10-20). */}
        <div className="aspect-square w-[71.5%] translate-x-[100px]">
          <div ref={yard} className="size-full will-change-[translate]">
            <Shipyard />
          </div>
        </div>
      </div>
    </div>
  );
}
