import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";
import { CircleCheck, CircleX, Info, Sparkles, TriangleAlert, X } from "lucide-react";
import { Button } from "./button";

// Something the person has to know, kept on the page: what is going on,
// what went well, what asks for care, what went wrong, what is offered.
// A moment's notice goes in a toast instead.

const alertVariants = cva(
  "group/alert relative grid w-full gap-y-0.5 rounded-lg border px-2.5 py-2 text-left text-sm has-data-[slot=alert-action]:pr-12 has-data-[slot=alert-icon]:grid-cols-[auto_1fr] has-data-[slot=alert-icon]:gap-x-2",
  {
    variants: {
      variant: {
        default: "border-border bg-card text-card-foreground *:data-[slot=alert-icon]:text-muted-foreground",
        info: "border-info/30 bg-info/10 *:data-[slot=alert-icon]:text-info",
        success: "border-success/30 bg-success/10 *:data-[slot=alert-icon]:text-success",
        warning: "border-warning/30 bg-warning/10 *:data-[slot=alert-icon]:text-warning",
        destructive:
          "border-destructive/30 bg-destructive/10 *:data-[slot=alert-icon]:text-destructive",
        upsell: "border-primary/30 bg-primary/10 *:data-[slot=alert-icon]:text-primary",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

type Variant = NonNullable<VariantProps<typeof alertVariants>["variant"]>;

// The icon each kind is known by. The default kind has none of its own.
const icons: Record<Variant, React.ReactElement | null> = {
  default: null,
  info: <Info />,
  success: <CircleCheck />,
  warning: <TriangleAlert />,
  destructive: <CircleX />,
  upsell: <Sparkles />,
};

function Alert({
  className,
  variant = "default",
  icon,
  onDismiss,
  children,
  ...props
}: React.ComponentProps<"div"> &
  VariantProps<typeof alertVariants> & {
    // An icon of its own; `null` for none.
    icon?: React.ReactElement | null;
    // With it, a button closes the alert.
    onDismiss?: () => void;
  }) {
  const kind = variant ?? "default";
  const leading = icon === undefined ? icons[kind] : icon;
  return (
    <div
      data-slot="alert"
      data-variant={kind}
      // What went wrong or asks for care interrupts; the rest is only told.
      role={kind === "destructive" || kind === "warning" ? "alert" : "status"}
      className={cn(alertVariants({ variant: kind }), className)}
      {...props}
    >
      {leading && (
        <span
          data-slot="alert-icon"
          className="row-span-full translate-y-0.5 [&_svg]:size-4 [&_svg]:shrink-0"
        >
          {leading}
        </span>
      )}
      {children}
      {onDismiss && (
        <AlertAction>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Dismiss"
            onClick={onDismiss}
            className="text-muted-foreground"
          >
            <X />
          </Button>
        </AlertAction>
      )}
    </div>
  );
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "font-medium group-has-data-[slot=alert-icon]/alert:col-start-2 [&_a]:underline [&_a]:underline-offset-3 [&_a]:hover:text-foreground",
        className,
      )}
      {...props}
    />
  );
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "text-sm text-balance text-muted-foreground group-has-data-[slot=alert-icon]/alert:col-start-2 md:text-pretty [&_a]:underline [&_a]:underline-offset-3 [&_a]:hover:text-foreground [&_p:not(:last-child)]:mb-4",
        className,
      )}
      {...props}
    />
  );
}

// What can be done about it: one or two buttons, under the text.
function AlertActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-actions"
      className={cn(
        "mt-2 flex flex-wrap items-center gap-2 group-has-data-[slot=alert-icon]/alert:col-start-2",
        className,
      )}
      {...props}
    />
  );
}

// One small thing in the corner: a close, a link.
function AlertAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-action"
      className={cn("absolute top-1.5 right-1.5", className)}
      {...props}
    />
  );
}

export { Alert, AlertTitle, AlertDescription, AlertActions, AlertAction };
