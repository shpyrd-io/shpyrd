"use client";

import * as React from "react";
import { Button } from "./button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";
import { Input } from "./input";
import { Label } from "./label";

// A dialog that asks before something is done. With `confirmation`, the
// action stays disabled until that text is typed: for what cannot be undone.
function ConfirmDialog({
  trigger,
  title,
  description,
  confirmation,
  label,
  action = "Confirm",
  cancel = "Cancel",
  variant = "default",
  onConfirm,
  open,
  defaultOpen = false,
  onOpenChange,
}: {
  // What opens the dialog: a button.
  trigger?: React.ReactElement;
  title: React.ReactNode;
  description?: React.ReactNode;
  // The text that has to be typed before the action is allowed.
  confirmation?: string;
  // What is written over the field; by default it asks for `confirmation`.
  label?: React.ReactNode;
  action?: React.ReactNode;
  cancel?: React.ReactNode;
  variant?: "default" | "destructive";
  onConfirm?: () => void;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const id = React.useId();
  const [typed, setTyped] = React.useState("");
  const [own, setOwn] = React.useState(defaultOpen);
  const allowed = confirmation === undefined || typed === confirmation;

  function change(next: boolean) {
    setOwn(next);
    if (!next) setTyped("");
    onOpenChange?.(next);
  }

  return (
    <Dialog open={open ?? own} onOpenChange={change}>
      {trigger && <DialogTrigger asChild>{trigger}</DialogTrigger>}
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (!allowed) return;
            onConfirm?.();
            change(false);
          }}
        >
          {confirmation !== undefined && (
            <div className="grid gap-2">
              <Label htmlFor={id} className="font-normal select-text">
                {label ?? (
                  <span>
                    Type <span className="font-mono font-medium">{confirmation}</span> to
                    confirm
                  </span>
                )}
              </Label>
              <Input
                id={id}
                value={typed}
                onChange={(event) => setTyped(event.target.value)}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
              />
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => change(false)}>
              {cancel}
            </Button>
            <Button type="submit" variant={variant} disabled={!allowed}>
              {action}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export { ConfirmDialog };
