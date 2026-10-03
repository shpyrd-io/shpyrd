"use client";

import { useRef } from "react";

// A page in a frame, in one theme: the page follows the system's theme
// once packed, and the library's class here, which the frame sets. The
// gallery's styles stay out of it.
export function Frame({ src, width, height, dark = false, title }: { src: string; width: number | string; height: number; dark?: boolean; title: string }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const theme = () => ref.current?.contentDocument?.documentElement.classList.toggle("dark", dark);
  return (
    <iframe
      ref={ref}
      title={title}
      src={src}
      onLoad={theme}
      style={{ width, height }}
      className="max-w-full shrink-0 rounded-lg border bg-background"
    />
  );
}
