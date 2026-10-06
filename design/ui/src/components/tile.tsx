import * as React from "react";
import { cn } from "cn";

// A small surface for an icon or mark. Give decorative icons aria-hidden.
function Tile({ size = "default", variant = "outline", className, ...props }: React.ComponentProps<"span"> & {
  size?: "sm" | "default" | "lg";
  variant?: "outline" | "muted";
}) {
  return <span data-slot="tile" data-size={size} className={cn("inline-flex shrink-0 items-center justify-center rounded-lg text-muted-foreground [&_svg]:shrink-0", size === "sm" ? "size-8 [&_svg]:size-4" : size === "lg" ? "size-12 [&_svg]:size-6" : "size-10 [&_svg]:size-5", variant === "outline" ? "border bg-background" : "bg-muted", className)} {...props} />;
}
export { Tile };
