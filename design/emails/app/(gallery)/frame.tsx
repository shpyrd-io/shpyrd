"use client";

import { useEffect, useRef, useState } from "react";

// An email in a frame, as tall as the email: its styles stay its own, and
// the gallery's stay out of it. The frame is of the same origin, so the
// email's height can be watched, and follows a change as it is saved.
export function Frame({ src, width, title }: { src: string; width: number; title: string }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(480);
  const [doc, setDoc] = useState<Document | null>(null);

  useEffect(() => {
    const email = doc?.getElementById("email");
    if (!email) return;
    const fit = () => setHeight(email.scrollHeight);
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(email);
    return () => watch.disconnect();
  }, [doc]);

  return (
    <iframe
      ref={ref}
      title={title}
      src={src}
      scrolling="no"
      onLoad={() => setDoc(ref.current?.contentDocument ?? null)}
      style={{ width, height }}
      className="max-w-full shrink-0 rounded-lg border bg-white"
    />
  );
}
