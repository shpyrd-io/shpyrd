import * as React from "react";
import { cn } from "cn";
import { Slot } from "radix-ui";
import { Label } from "./label";

// A label and what it is the label of, with what explains it and what is
// wrong with it. The field inside is tied to all of them: it takes the
// `id` the label points to, and is told of the hint and of the error.
function Field({
  className,
  children,
  label,
  labelHidden = false,
  hint,
  error,
  required = false,
  id,
  ...props
}: React.ComponentProps<"div"> & {
  label: React.ReactNode;
  // The label is there for who cannot see, and not drawn.
  labelHidden?: boolean;
  // What explains the field, under it.
  hint?: React.ReactNode;
  // What is wrong with what was typed. The field is marked as not valid.
  error?: React.ReactNode;
  required?: boolean;
  // The field itself: an input, a select.
  children: React.ReactElement<{ id?: string }>;
}) {
  const own = React.useId();
  // The `id` the field already has is kept, so the label points to it.
  const control = id ?? children.props.id ?? own;
  const hintId = `${control}-hint`;
  const errorId = `${control}-error`;
  const told: Record<string, unknown> = {
    id: control,
    "aria-describedby":
      [hint && hintId, error && errorId].filter(Boolean).join(" ") || undefined,
    "aria-invalid": error ? true : undefined,
    required: required || undefined,
  };
  return (
    <div
      data-slot="field"
      data-invalid={error ? true : undefined}
      className={cn("grid gap-2", className)}
      {...props}
    >
      <Label htmlFor={control} className={cn(labelHidden && "sr-only")}>
        {label}
        {required && (
          <span aria-hidden className="text-destructive">
            *
          </span>
        )}
      </Label>
      <Slot.Root {...told}>{children}</Slot.Root>
      {hint && (
        <p id={hintId} data-slot="field-hint" className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} data-slot="field-error" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

export { Field };
