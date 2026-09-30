"use client";

import * as React from "react";
import { cn } from "cn";
import { initials, letters, shape, type Size } from "../lib/avatar";

// The picture of someone, or of something: a person is a circle; a team,
// a bot or an organisation is a square. Without a picture, the initials.

function Avatar({
  className,
  src,
  alt,
  size = 20,
  square = false,
  style,
  ...props
}: Omit<React.ComponentProps<"span">, "children"> & {
  src?: string;
  // The name: what is read aloud, and what is drawn without a picture.
  alt: string;
  size?: Size;
  square?: boolean;
}) {
  // A picture that could not be loaded gives way to the initials.
  const [failed, setFailed] = React.useState<string>();
  const picture = src !== undefined && failed !== src;
  return (
    <span
      data-slot="avatar"
      data-shape={square ? "square" : "circle"}
      role={picture ? undefined : "img"}
      aria-label={picture ? undefined : alt}
      className={cn(
        "inline-flex shrink-0 items-center justify-center overflow-hidden bg-muted align-middle font-medium leading-none text-muted-foreground select-none",
        shape(size, square),
        className,
      )}
      style={{ width: size, height: size, fontSize: letters(size), ...style }}
      {...props}
    >
      {picture ? (
        <img
          src={src}
          alt={alt}
          width={size}
          height={size}
          className="size-full object-cover"
          onError={() => setFailed(src)}
        />
      ) : (
        initials(alt)
      )}
    </span>
  );
}

export { Avatar };
