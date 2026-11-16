"use client";

// Where the pointer is over a block, for its edge to light from there
// (feature-blocks.css, [data-tile]): sets --mx and --my on the block under
// the pointer, in pixels from its corner. One listener on the blocks'
// container, drawn as an empty span in it.
import { useEffect, useRef } from "react";

export function PointerEdge() {
  const self = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const box = self.current?.parentElement;
    if (!box) return;
    const move = (e: PointerEvent) => {
      const tile = (e.target as Element | null)?.closest<HTMLElement>("[data-tile]");
      if (!tile || !box.contains(tile)) return;
      const r = tile.getBoundingClientRect();
      // The block grows a little under the pointer: back to its own size.
      const k = tile.offsetWidth / r.width;
      tile.style.setProperty("--mx", `${(e.clientX - r.left) * k}px`);
      tile.style.setProperty("--my", `${(e.clientY - r.top) * k}px`);
    };
    box.addEventListener("pointermove", move, { passive: true });
    return () => box.removeEventListener("pointermove", move);
  }, []);
  return <span ref={self} hidden />;
}
