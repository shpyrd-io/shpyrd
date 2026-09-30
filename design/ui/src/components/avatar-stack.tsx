import * as React from "react";
import { cn } from "cn";
import { Avatar } from "./avatar";
import { letters, shape, type Size } from "../lib/avatar";

// Who is in something: the pictures over each other, the first on top.
// Under the pointer they spread, so each can be seen. Past the maximum,
// the last place says how many more there are.

export type Person = { src?: string; alt: string; square?: boolean };

function AvatarStack({
  className,
  avatars,
  size = 20,
  square = false,
  max = 4,
  alignRight = false,
  expand = true,
  style,
  ...props
}: React.ComponentProps<"div"> & {
  avatars: Person[];
  size?: Size;
  // For all of them; one may say otherwise.
  square?: boolean;
  // How many places there are, the count included.
  max?: number;
  // The stack hugs the right, the first on the right.
  alignRight?: boolean;
  // Spread under the pointer.
  expand?: boolean;
}) {
  const overflow = avatars.length > max;
  const shown = overflow ? avatars.slice(0, Math.max(1, max - 1)) : avatars;
  const rest = avatars.slice(shown.length);
  const places = shown.length + (overflow ? 1 : 0);

  // What each place after the first does: overlaps, then spreads.
  const place = (i: number) =>
    cn(
      "relative ring-2 ring-background transition-[margin] duration-200 ease-out",
      i > 0 && (alignRight ? "mr-(--overlap)" : "ml-(--overlap)"),
      i > 0 && expand && (alignRight ? "group-hover/stack:mr-1" : "group-hover/stack:ml-1"),
    );

  return (
    <div
      role="group"
      data-slot="avatar-stack"
      data-expand={expand}
      className={cn(
        "group/stack inline-flex items-center",
        alignRight ? "flex-row-reverse" : "flex-row",
        className,
      )}
      style={{ "--overlap": `${-Math.round(size / 2)}px`, ...style } as React.CSSProperties}
      {...props}
    >
      {shown.map((person, i) => (
        <Avatar
          key={i}
          src={person.src}
          alt={person.alt}
          size={size}
          square={person.square ?? square}
          title={person.alt}
          className={place(i)}
          style={{ zIndex: places - i }}
        />
      ))}
      {overflow && (
        <span
          data-slot="avatar-stack-count"
          role="img"
          aria-label={`${rest.length} more`}
          title={rest.map((p) => p.alt).join(", ")}
          className={cn(
            "inline-flex shrink-0 items-center justify-center bg-muted font-medium leading-none text-muted-foreground tabular-nums select-none",
            shape(size, square),
            place(shown.length),
          )}
          style={{ width: size, height: size, fontSize: letters(size), zIndex: 0 }}
        >
          +{rest.length}
        </span>
      )}
    </div>
  );
}

export { AvatarStack };
