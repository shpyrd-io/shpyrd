"use client";

import * as React from "react";
import { cn } from "cn";

// Six boxes, one digit each, read as one field. Typing moves to the next
// box, deleting goes back, and a code pasted into any box fills them all.
// The value is the digits typed so far, in order.
function CodeInput({
  value,
  onChange,
  onComplete,
  // The name of each box for who cannot see: "Digit 1 of 6".
  label = (i, n) => `Digit ${i} of ${n}`,
  // What the boxes are called in the markup, the id and the name of each:
  // `code-1` to `code-6`. What analytics knows them by.
  name = "code",
  length = 6,
  autoFocus,
  disabled,
  invalid,
  className,
  ...props
}: Omit<React.ComponentProps<"div">, "onChange"> & {
  value: string;
  onChange: (value: string) => void;
  // Called once every box holds a digit.
  onComplete?: (value: string) => void;
  label?: (i: number, n: number) => string;
  name?: string;
  length?: number;
  autoFocus?: boolean;
  disabled?: boolean;
  invalid?: boolean;
}) {
  const boxes = React.useRef<(HTMLInputElement | null)[]>([]);
  const digits = value.replace(/\D/g, "").slice(0, length);

  function focus(i: number) {
    boxes.current[Math.max(0, Math.min(length - 1, i))]?.focus();
  }

  function set(next: string) {
    onChange(next);
    if (next.length === length) onComplete?.(next);
  }

  return (
    <div
      role="group"
      className={cn("flex justify-center gap-2.5", className)}
      onPaste={(e) => {
        const pasted = e.clipboardData.getData("text").replace(/\D/g, "").slice(0, length);
        if (!pasted) return;
        e.preventDefault();
        set(pasted);
        focus(pasted.length);
      }}
      {...props}
    >
      {Array.from({ length }, (_, i) => (
        <input
          key={i}
          ref={(el) => {
            boxes.current[i] = el;
          }}
          id={`${name}-${i + 1}`}
          name={`${name}-${i + 1}`}
          type="text"
          inputMode="numeric"
          autoComplete={i === 0 ? "one-time-code" : "off"}
          aria-label={label(i + 1, length)}
          aria-invalid={invalid || undefined}
          autoFocus={autoFocus && i === 0}
          disabled={disabled}
          maxLength={length}
          value={digits[i] ?? ""}
          onFocus={(e) => e.target.select()}
          onKeyDown={(e) => {
            if (e.key === "Backspace") {
              e.preventDefault();
              if (digits[i]) set(digits.slice(0, i) + digits.slice(i + 1));
              else {
                set(digits.slice(0, Math.max(0, i - 1)));
                focus(i - 1);
              }
            } else if (e.key === "ArrowLeft") focus(i - 1);
            else if (e.key === "ArrowRight") focus(i + 1);
          }}
          onChange={(e) => {
            const typed = e.target.value.replace(/\D/g, "");
            if (!typed) return;
            // A filled box clicked with the pointer has lost its selection:
            // its digit arrives beside the one typed, before or after it.
            const old = digits[i];
            if (old && typed.length === 2) {
              set(digits.slice(0, i) + (typed[0] === old ? typed[1] : typed[0]) + digits.slice(i + 1));
              focus(i + 1);
              return;
            }
            // The browser's own one-time-code fill lands here whole.
            const next = (digits.slice(0, i) + typed + digits.slice(i + typed.length)).slice(0, length);
            set(next);
            focus(i + typed.length);
          }}
          className={cn(
            "size-14 rounded-xl border border-input bg-transparent text-center font-heading text-2xl font-medium text-foreground tabular-nums caret-foreground transition-colors outline-none",
            "focus-visible:border-foreground focus-visible:ring-3 focus-visible:ring-foreground/10",
            "disabled:cursor-not-allowed disabled:opacity-50",
            "aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20",
            "dark:bg-input/30 dark:focus-visible:border-foreground/70",
          )}
        />
      ))}
    </div>
  );
}

export { CodeInput };
