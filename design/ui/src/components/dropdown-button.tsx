import * as React from "react";
import { cn } from "cn";
import { ChevronDownIcon } from "lucide-react";
import { Button } from "./button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "./dropdown-menu";

// The size of the part that holds the chevron, when the button is split.
const chevronSize = {
  default: "icon",
  xs: "icon-xs",
  sm: "icon-sm",
  lg: "icon-lg",
} as const;

// A button that opens a menu. It takes what a button takes; its children
// are the items of the menu. With `onClick` it is split in two parts: the
// text does the default action and the chevron opens the menu.
function DropdownButton({
  label,
  children,
  align,
  contentClassName,
  menuLabel = "More options",
  open,
  defaultOpen,
  onOpenChange,
  onClick,
  className,
  variant = "default",
  size = "default",
  disabled,
  ...props
}: Omit<
  React.ComponentProps<typeof Button>,
  "children" | "iconEnd" | "asChild" | "size"
> &
  Pick<
    React.ComponentProps<typeof DropdownMenu>,
    "open" | "defaultOpen" | "onOpenChange"
  > & {
    label?: React.ReactNode;
    children: React.ReactNode;
    size?: keyof typeof chevronSize;
    align?: React.ComponentProps<typeof DropdownMenuContent>["align"];
    contentClassName?: string;
    // What the chevron is called when it is a part by itself.
    menuLabel?: string;
  }) {
  const split = onClick !== undefined;
  const chevron = (
    <ChevronDownIcon className="transition-transform group-aria-expanded/button:rotate-180" />
  );
  const content = (
    <DropdownMenuContent
      align={align ?? (split ? "end" : "start")}
      className={cn(
        "w-auto min-w-[max(8rem,var(--radix-dropdown-menu-trigger-width))]",
        contentClassName,
      )}
    >
      {children}
    </DropdownMenuContent>
  );

  if (!split) {
    return (
      <DropdownMenu open={open} defaultOpen={defaultOpen} onOpenChange={onOpenChange}>
        <DropdownMenuTrigger asChild disabled={disabled}>
          <Button
            variant={variant}
            size={size}
            disabled={disabled}
            className={className}
            iconEnd={chevron}
            {...props}
          >
            {label}
          </Button>
        </DropdownMenuTrigger>
        {content}
      </DropdownMenu>
    );
  }

  return (
    <div
      data-slot="button-group"
      className={cn("inline-flex", variant !== "outline" && "gap-px", className)}
    >
      <Button
        variant={variant}
        size={size}
        disabled={disabled}
        onClick={onClick}
        className="rounded-r-none focus-visible:z-10 in-data-[slot=button-group]:rounded-r-none"
        {...props}
      >
        {label}
      </Button>
      <DropdownMenu open={open} defaultOpen={defaultOpen} onOpenChange={onOpenChange}>
        <DropdownMenuTrigger asChild disabled={disabled}>
          <Button
            variant={variant}
            size={chevronSize[size]}
            disabled={disabled}
            aria-label={menuLabel}
            icon={chevron}
            className={cn(
              "rounded-l-none focus-visible:z-10 in-data-[slot=button-group]:rounded-l-none",
              variant === "outline" && "-ml-px",
            )}
          />
        </DropdownMenuTrigger>
        {content}
      </DropdownMenu>
    </div>
  );
}

export { DropdownButton };
