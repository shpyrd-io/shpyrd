"use client";

import { useEffect } from "react";

/**
 * The workspace's accent colour (RFC-0033 branding): set on the root as
 * the theme's --primary while a page that knows the workspace is mounted.
 */
export function useBrandColor(color?: string) {
  useEffect(() => {
    const root = document.documentElement;
    if (color) {
      root.style.setProperty("--primary", color);
      root.style.setProperty("--primary-foreground", "#ffffff");
      root.style.setProperty("--ring", color);
    } else {
      root.style.removeProperty("--primary");
      root.style.removeProperty("--primary-foreground");
      root.style.removeProperty("--ring");
    }
  }, [color]);
}
