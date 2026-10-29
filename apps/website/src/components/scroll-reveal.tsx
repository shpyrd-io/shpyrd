"use client";

import { useEffect } from "react";

// What comes into view as the page is scrolled rises into place, after
// apple.com/iphone-duo (Giovani, 2026-10-20): a section's label, heading and
// line one after the other, and the items of its lists and blocks the same
// way, 80ms apart, once, the first time they come on the screen. What is on
// the screen when the page opens (the hero) is left as it is, and so is
// everything for who asked the system for less motion. The browser says when
// something comes into view; nothing runs while scrolling (src/styles/global.css).
const TARGETS = [
  "[data-slot=section-intro] > *",
  "ul > li",
  "ol > li",
  ".fb > *",
  "[role=tablist] > *",
  "[data-slot=cta]",
  "[data-slot=river]",
  // The blocks of the other marketing pages (features, solutions, pricing,
  // getting started; Giovani, 2026-10-20): the checklist, the cards of a
  // grid laid straight in a section, the questions, the simple closing
  // call's buttons and links.
  "[data-slot=checklist]",
  // (not a section's heading block or the home's bento, whose parts come in
  // on their own)
  "[data-slot=stack] > .grid > :not([data-slot=section-intro]):not(.fb)",
  "details",
  "[data-slot=next-step] [data-slot=stack] > div:not([data-slot]):not(.aurora)",
  "[data-slot=next-step] > nav",
  // The words of the orange closing call, one after the other (Giovani,
  // 2026-10-20); its yard comes in its own way (shipyard-bar.tsx).
  "[data-slot=cta-band] [data-slot=hero-heading]",
  "[data-slot=cta-band] [data-slot=hero-description]",
  "[data-slot=cta-band] [data-slot=hero-actions]",
  "[data-slot=cta-band] [data-slot=hero-note]",
  // Anything else marked to come in so.
  "[data-reveal-item]",
].join(",");

// What comes in whole, its parts not moving on their own: what lays itself
// out by measuring its parts (the loop's track, the business picture set
// level with the words beside it).
// The checklist too, a card with its picture: its list does not come in
// apart from it.
const WHOLE = "[data-reveal-whole], [data-slot=river], [data-slot=checklist]";

// Not inside the films, the header, the footer or what is only drawn, nor
// inside something that comes in whole.
// Nor what comes in its own way (the routes' steps, with their line).
const SKIP = `.stage, header, footer, nav:not([data-slot=next-step] > nav), [aria-hidden=true], [data-slot=app-window], svg, [data-reveal-skip], ${WHOLE}`;

const STEP = 80;
const MOST = 6;

export function ScrollReveal() {
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const root = document.querySelector("main") ?? document.body;
    const fold = window.innerHeight;
    const items = Array.from(root.querySelectorAll<HTMLElement>(TARGETS)).filter(
      (el) =>
        !el.parentElement?.closest(SKIP) &&
        (!el.matches(SKIP) || el.matches(WHOLE)) &&
        el.getBoundingClientRect().top > fold,
    );

    const timers: number[] = [];
    const sight = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const el = e.target as HTMLElement;
          sight.unobserve(el);
          el.dataset.reveal = "shown";
          // Once in place it is itself again: its own transitions and moves.
          const delay = Number(el.style.getPropertyValue("--reveal-i") || 0) * STEP;
          timers.push(window.setTimeout(() => el.removeAttribute("data-reveal"), delay + 900));
        }
      },
      { rootMargin: "0px 0px -8% 0px" },
    );

    for (const el of items) {
      const siblings = el.parentElement ? Array.from(el.parentElement.children) : [el];
      el.style.setProperty("--reveal-i", String(Math.min(siblings.indexOf(el), MOST)));
      el.dataset.reveal = "";
      sight.observe(el);
    }
    return () => {
      sight.disconnect();
      timers.forEach((t) => window.clearTimeout(t));
      for (const el of items) el.removeAttribute("data-reveal");
    };
  }, []);

  return null;
}
