import * as React from "react";
import { cn } from "cn";
import { Slot } from "radix-ui";

function Card({
  className,
  size = "default",
  variant = "default",
  asChild = false,
  interactive = false,
  background,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  size?: "default" | "sm";
  // `secondary` lifts the card from the page with a shadow.
  variant?: "default" | "secondary" | "dashed";
  // The card becomes its child: a link, and it answers to the pointer.
  asChild?: boolean;
  interactive?: boolean;
  // Decorative layer behind the content. The app controls its artwork.
  background?: React.ReactNode;
}) {
  const Comp = asChild ? Slot.Root : "div";

  return (
    <Comp
      data-slot="card"
      data-size={size}
      data-interactive={interactive || asChild || undefined}
      data-variant={variant}
      className={cn(
        "group/card flex flex-col gap-(--card-spacing) overflow-hidden rounded-xl bg-card py-(--card-spacing) text-sm text-card-foreground ring-1 ring-foreground/10 outline-none [--card-spacing:--spacing(4)] has-data-[slot=card-footer]:pb-0 has-[>img:first-child]:pt-0 data-[size=sm]:[--card-spacing:--spacing(3)] data-[size=sm]:has-data-[slot=card-footer]:pb-0 data-[variant=secondary]:shadow-md dark:data-[variant=secondary]:shadow-black/50 *:[img:first-child]:rounded-t-xl *:[img:last-child]:rounded-b-xl [a]:transition-shadow [a]:hover:ring-primary [a]:focus-visible:ring-2 [a]:focus-visible:ring-primary",
        "relative isolate data-[variant=dashed]:border data-[variant=dashed]:border-dashed data-[variant=dashed]:border-border data-[variant=dashed]:ring-0 data-[interactive=true]:transition-[background-color,box-shadow] data-[interactive=true]:duration-fast data-[interactive=true]:hover:bg-muted/50 data-[interactive=true]:hover:ring-primary data-[interactive=true]:focus-visible:ring-2 data-[interactive=true]:focus-visible:ring-primary",
        className,
      )}
      {...props}
    >
      {background && <span data-slot="card-background" aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">{background}</span>}
      {asChild ? <Slot.Slottable>{children}</Slot.Slottable> : children}
    </Comp>
  );
}

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "group/card-header @container/card-header grid auto-rows-min items-start gap-1 rounded-t-xl px-(--card-spacing) has-data-[slot=card-action]:grid-cols-[1fr_auto] has-data-[slot=card-description]:grid-rows-[auto_auto] [.border-b]:pb-(--card-spacing)",
        className,
      )}
      {...props}
    />
  );
}

function CardTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-title"
      className={cn(
        "font-heading text-base leading-snug font-medium group-data-[size=sm]/card:text-sm",
        className,
      )}
      {...props}
    />
  );
}

function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}

function CardAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        "col-start-2 row-span-2 row-start-1 self-start justify-self-end",
        className,
      )}
      {...props}
    />
  );
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("px-(--card-spacing)", className)}
      {...props}
    />
  );
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "flex items-center rounded-b-xl border-t bg-muted/50 p-(--card-spacing)",
        className,
      )}
      {...props}
    />
  );
}

export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardAction,
  CardDescription,
  CardContent,
};
