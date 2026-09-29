"use client";

import * as React from "react";
import { cn } from "cn";

function Input({
  className,
  type,
  icon,
  iconEnd,
  prefix,
  suffix,
  ...props
}: Omit<React.ComponentProps<"input">, "prefix"> & {
  // An icon before the text.
  icon?: React.ReactElement;
  // An icon after the text.
  iconEnd?: React.ReactElement;
  // What is written before the text, in grey: `https://`.
  prefix?: React.ReactNode;
  // What is written after the text, in grey: `.shpyrd.app`.
  suffix?: React.ReactNode;
}) {
  if (!icon && !iconEnd && !prefix && !suffix) {
    return (
      <input
        type={type}
        data-slot="input"
        className={cn(
          "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
          className,
        )}
        {...props}
      />
    );
  }

  // With something beside the text, the box is drawn around both and the
  // field inside has no border of its own.
  return (
    <div
      data-slot="input-box"
      className={cn(
        "flex h-8 w-full min-w-0 cursor-text items-center gap-1.5 rounded-lg border border-input bg-transparent px-2.5 text-base text-muted-foreground transition-colors has-[input:focus-visible]:border-ring has-[input:focus-visible]:ring-3 has-[input:focus-visible]:ring-ring/50 has-[input:disabled]:pointer-events-none has-[input:disabled]:bg-input/50 has-[input:disabled]:opacity-50 has-[input[aria-invalid=true]]:border-destructive has-[input[aria-invalid=true]]:ring-3 has-[input[aria-invalid=true]]:ring-destructive/20 md:text-sm dark:bg-input/30 dark:has-[input:disabled]:bg-input/80 dark:has-[input[aria-invalid=true]]:border-destructive/50 dark:has-[input[aria-invalid=true]]:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      onPointerDown={(event) => {
        if ((event.target as HTMLElement).closest("input, button, a")) return;
        event.preventDefault();
        event.currentTarget.querySelector("input")?.focus();
      }}
    >
      {icon}
      {prefix && (
        <span data-slot="input-prefix" className="shrink-0 select-none">
          {prefix}
        </span>
      )}
      <input
        type={type}
        data-slot="input"
        className="h-full w-full min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
        {...props}
      />
      {suffix && (
        <span data-slot="input-suffix" className="shrink-0 select-none">
          {suffix}
        </span>
      )}
      {iconEnd}
    </div>
  );
}

export { Input };
