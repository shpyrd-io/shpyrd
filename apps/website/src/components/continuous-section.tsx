"use client";

import * as React from "react";
import { SegmentedNav } from "@shpyrd/ui/components/segmented-nav";
import { cn } from "@shpyrd/ui/lib/cn";
import { BinaryMark } from "@/components/binary-mark";

// A section of the site read as one page (Giovani, 2026-10-08). Its
// subpages follow one another down the page, parted by a hairline; the bar of
// its subpages stays under the site's header while the page scrolls, lights
// the one on the screen, and takes the reader to one when it is pressed. The
// address follows the reading (#slug), so a subpage can still be shared.
//
// Each part is a subpage as it is, its own hero included; the subpages'
// "Next" endings are hidden (a page read on needs none), and every hero after
// the first is drawn at a section title's size, so the page has one opening.
export function ContinuousSection({
  label,
  parts,
  ending,
}: {
  label: string;
  parts: { slug: string; title: string; content: React.ReactNode }[];
  // What closes the whole page, after its last part.
  ending?: React.ReactNode;
}) {
  const [current, setCurrent] = React.useState(parts[0].slug);
  // While a press on the bar scrolls the page, the bar does not follow it.
  const going = React.useRef<number | null>(null);

  React.useEffect(() => {
    const els = parts.map((p) => document.getElementById(p.slug)).filter(Boolean) as HTMLElement[];
    const seen = () => {
      if (going.current !== null) return;
      // The part whose top has passed a line under the bars.
      const line = window.innerHeight * 0.4;
      let at = parts[0].slug;
      for (const el of els) if (el.getBoundingClientRect().top <= line) at = el.id;
      setCurrent(at);
    };
    seen();
    window.addEventListener("scroll", seen, { passive: true });
    return () => window.removeEventListener("scroll", seen);
  }, [parts]);

  // The address follows the part being read, so it can be shared.
  React.useEffect(() => {
    const want = current === parts[0].slug ? location.pathname : `#${current}`;
    if (location.hash !== (current === parts[0].slug ? "" : `#${current}`)) history.replaceState(null, "", want);
  }, [current, parts]);

  // Arriving with an address that names a part: go to it.
  React.useEffect(() => {
    const id = location.hash.slice(1);
    if (id) document.getElementById(id)?.scrollIntoView();
  }, []);

  const go = (slug: string) => (event: React.MouseEvent) => {
    event.preventDefault();
    setCurrent(slug);
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (going.current) window.clearTimeout(going.current);
    going.current = window.setTimeout(() => (going.current = null), less ? 50 : 900);
    document.getElementById(slug)?.scrollIntoView({ behavior: less ? "auto" : "smooth" });
  };

  return (
    <>
      {/* Behind the top of every section page: the shpyrd mark, a ship seen
          from the front, made of falling binary (binary-mark.tsx), the same
          size and place on all of them, scrolling with the page: in the
          middle, 991px tall, its top level with the section bar's foot
          (160px). (Until 2026-10-09: 847px, its top at 100px, 370px
          right of the middle; centred heroes 1101px at 172px.) The rain
          falls softly just around it too, four cells out, and it leans
          softly toward the pointer. */}
      <BinaryMark height={991} offset={160} x={0} near={4} tilt />
      {/* The bar, under the header (its 72px and 12px more), over the page. */}
      <div className="sticky top-[84px] z-30 mx-auto w-full max-w-7xl px-4 pt-8 @3xl/page-layout:px-6">
        {/* What scrolls under the bar is hidden behind it: the page's own
            background, solid from the header to the bar's foot, across the
            whole width, then fading out softly, eased, in the 96px under it. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute left-1/2 w-screen -translate-x-1/2 -top-[84px] -bottom-24 -z-10 bg-[linear-gradient(to_bottom,var(--background)_calc(100%-96px),color-mix(in_oklab,var(--background)_85%,transparent)_calc(100%-64px),color-mix(in_oklab,var(--background)_45%,transparent)_calc(100%-32px),transparent)]"
        />
        <SegmentedNav
          aria-label={label}
          links={parts.map((p) => (
            <a key={p.slug} href={`#${p.slug}`} onClick={go(p.slug)} aria-current={current === p.slug ? "page" : undefined}>
              {p.title}
            </a>
          ))}
        />
      </div>
      {parts.map((p, i) => (
        <React.Fragment key={p.slug}>
          {i > 0 && (
            // The hairline between two parts: as far from the last block
            // of one part (93px) as from the next part's title (93px).
            <div aria-hidden="true" className="mx-auto w-full max-w-[calc(80rem-3rem)] px-4 md:px-0">
              <div className="relative mt-[69px] mb-[4px]">
                {/* The line, fading out softly toward both its ends. */}
                <hr className="h-px border-0 bg-[linear-gradient(to_right,transparent,color-mix(in_oklab,var(--foreground)_12%,transparent)_30%,color-mix(in_oklab,var(--foreground)_12%,transparent)_70%,transparent)] dark:bg-[linear-gradient(to_right,transparent,color-mix(in_oklab,var(--foreground)_22%,transparent)_30%,color-mix(in_oklab,var(--foreground)_22%,transparent)_70%,transparent)]" />
                {/* The docs menu's mark of the open page (3 × 16px, orange),
                    lying down in the middle of the line: a part begins. */}
                <span className="absolute top-1/2 left-1/2 h-[3px] w-4 -translate-x-1/2 -translate-y-1/2 rounded-[2px] bg-primary" />
              </div>
            </div>
          )}
        <section
          id={p.slug}
          aria-label={p.title}
          className={cn(
            // Its top clear of the header and the bar when it is gone to.
            "scroll-mt-[76px]",
            // Endings of the subpages, and their own CTA blocks, are not read
            // in the middle of the page.
            "[&_[data-slot=next-step]]:hidden [&_[data-slot=cta]]:hidden",
            // The titles in three sizes, so each kind reads as what it is:
            // the page's opening 60px, each part's title 48px, and the titles
            // of the blocks inside a part 36px.
            // The page's own hero stands over the binary mark: its words in
            // the page's glow (global.css), its buttons as they are.
            i === 0 && "hero-glow",
            i > 0 &&
              "[&_[data-slot=hero-heading]]:!text-4xl @2xl/hero:[&_[data-slot=hero-heading]]:!text-5xl [&_[data-slot=hero]]:!pt-0",
            "[&_[data-slot=section-intro-heading]]:!text-3xl @2xl/section-intro:[&_[data-slot=section-intro-heading]]:!text-4xl",
            // The room in three steps too: 168px between parts (the line
            // between them), 112px between the blocks of a part, and 40px
            // from a block's title to what it shows.
            "[&>*>*+*]:!mt-12 [&>*>[data-slot=hero]+*]:!mt-0 [&>*>*>[data-slot=section-intro]+*]:!mt-4",
            // A part's last block keeps none of a page's 144px over the footer:
            // the line after it sets the room.
            "[&>*>*]:!mb-0",
          )}
        >
          {p.content}
        </section>
        </React.Fragment>
      ))}
      {/* The page's ending, as far from the last part as the parts are from
          one another's blocks: 168px to a closing block's title, and the
          shipyard's drawing clear of what is over it. */}
      {ending && <div className="mt-[120px]">{ending}</div>}
    </>
  );
}

// An old address of a part: the static site has no server to redirect, so
// the page itself sends the reader on, and says where for who has no script.
export function Redirect({ to }: { to: string }) {
  React.useEffect(() => {
    window.location.replace(to);
  }, [to]);
  return (
    <>
      <meta httpEquiv="refresh" content={`0; url=${to}`} />
      <p className="p-8 text-center text-muted-foreground">
        This page is now part of <a href={to} className="text-foreground underline underline-offset-4">one page</a>.
      </p>
    </>
  );
}
