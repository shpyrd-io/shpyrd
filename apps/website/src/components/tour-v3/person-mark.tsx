import { cn } from "@shpyrd/ui/lib/cn";

// A small drawn person in place of initials (Giovani, 2026-10-28): a woman
// for Marina, a man for Diego, a figure with no face for someone we don't
// know. Flat, in soft tones, inside a circle the size of an avatar.
// Two more for slide 10 (Giovani, 2026-10-29: no one twice): a woman with
// her hair in a bun, a man with a beard and glasses, each in tones of their
// own.
export type Look = "woman" | "man" | "anyone" | "woman-bun" | "man-beard";

// The tones of each person drawn: skin, neck (a shade under it), hair, shirt.
const tones: Record<
  Exclude<Look, "anyone">,
  [string, string, string, string]
> = {
  woman: ["#f2c4a0", "#e8b48f", "#3b2a24", "#ff7a3d"],
  man: ["#f2c4a0", "#e8b48f", "#2c2420", "#5b6b8c"],
  "woman-bun": ["#c98e66", "#b97d57", "#1f1a17", "#2a9d8f"],
  "man-beard": ["#8d5a3b", "#7a4c31", "#1b1512", "#3f7d4e"],
};

export function PersonMark({
  look,
  className,
}: {
  look: Look;
  className?: string;
}) {
  return (
    <svg
      viewBox="0 0 40 40"
      aria-hidden="true"
      className={cn("size-10 shrink-0 overflow-hidden rounded-full", className)}
    >
      <circle
        cx="20"
        cy="20"
        r="20"
        className={look === "anyone" ? "fill-muted" : "fill-primary/10"}
      />
      {look === "anyone" ? (
        <g className="fill-muted-foreground/45">
          <circle cx="20" cy="16" r="6.5" />
          <path d="M7 37c1.5-7.5 6.8-11.5 13-11.5S31.5 29.5 33 37a20 20 0 0 1-26 0Z" />
        </g>
      ) : (
        <g>
          {/* Hair behind the head: long for her, short for him; a bun on
              top for the woman with one. */}
          {look === "woman" ? (
            <path
              d="M11.5 19c0-6.4 3.9-10.5 8.5-10.5s8.5 4.1 8.5 10.5v9.5h-17Z"
              fill="#3b2a24"
            />
          ) : null}
          {look === "woman-bun" ? (
            <circle cx="20" cy="8.6" r="3.6" fill={tones[look][2]} />
          ) : null}
          {/* Shoulders, in a shirt. */}
          <path
            d="M7 37c1.6-7 6.9-10.8 13-10.8S31.4 30 33 37a20 20 0 0 1-26 0Z"
            fill={tones[look][3]}
          />
          {/* Neck and head. */}
          <rect
            x="17.5"
            y="21"
            width="5"
            height="6"
            rx="2"
            fill={tones[look][1]}
          />
          <circle cx="20" cy="17" r="6.5" fill={tones[look][0]} />
          {look === "woman-bun" ? (
            <path
              d="M13.4 16c.3-4.3 3.1-6.8 6.6-6.8s6.3 2.5 6.6 6.8c-1.9-1.9-4.1-2.8-6.6-2.8s-4.7.9-6.6 2.8Z"
              fill={tones[look][2]}
            />
          ) : look === "man-beard" ? (
            <g fill={tones[look][2]}>
              <path d="M13.4 15.5c.2-4.2 3-6.6 6.6-6.6s6.4 2.4 6.6 6.6c-1.7-1.6-3.8-2.4-6.6-2.4s-4.9.8-6.6 2.4Z" />
              {/* The beard, round the chin. */}
              <path d="M14 18.2c.5 3.9 3 6 6 6s5.5-2.1 6-6c-1.7 1.4-3.7 2.1-6 2.1s-4.3-.7-6-2.1Z" />
              {/* The glasses. */}
              <g fill="none" stroke={tones[look][2]} strokeWidth="0.9">
                <circle cx="17.3" cy="17.2" r="1.9" />
                <circle cx="22.7" cy="17.2" r="1.9" />
                <path d="M19.2 17.2h1.6" />
              </g>
            </g>
          ) : look === "woman" ? (
            <path
              d="M13.3 16.5c.4-4.4 3.2-7 6.7-7s6.3 2.6 6.7 7c-2.6-.4-5.4-1.9-7-4.1-1.2 2-3.6 3.6-6.4 4.1Z"
              fill="#3b2a24"
            />
          ) : (
            <path
              d="M13.4 15.5c.2-4.2 3-6.6 6.6-6.6s6.4 2.4 6.6 6.6c-1.7-1.6-3.8-2.4-6.6-2.4s-4.9.8-6.6 2.4Z"
              fill="#2c2420"
            />
          )}
        </g>
      )}
    </svg>
  );
}
