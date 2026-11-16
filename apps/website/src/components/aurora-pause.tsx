"use client";

// The CTA block's light (src/styles/aurora.css) still while it is out of
// view (Giovani, 2026-11-16: the subpages' performance): its rays turn a
// gradient under a blur, drawn again at every frame. Put last in the
// .aurora; it marks it [data-paused] while it is not on screen.
import { useEffect, useRef } from "react";

export function AuroraPause() {
  const self = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const aurora = self.current?.parentElement;
    if (!aurora) return;
    const sight = new IntersectionObserver(([e]) => {
      if (e.isIntersecting) delete aurora.dataset.paused;
      else aurora.dataset.paused = "";
    });
    sight.observe(aurora);
    return () => sight.disconnect();
  }, []);
  return <span ref={self} hidden />;
}
