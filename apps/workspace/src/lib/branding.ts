"use client";

import { useEffect } from "react";

// The accent colour of the workspace, set on the root as the theme's
// primary while a page that knows the workspace is up: the launcher and
// the sign-in page, where its people arrive.
export function useBrandColor(color?: string) {
  useEffect(() => {
    const root = document.documentElement;
    if (color) {
      root.style.setProperty("--primary", color);
      root.style.setProperty("--primary-foreground", "#ffffff");
      root.style.setProperty("--ring", color);
      root.style.setProperty("--primary-light", `color-mix(in srgb, ${color}, white 60%)`);
      root.style.setProperty(
        "--primary-light-soft",
        `color-mix(in srgb, color-mix(in srgb, ${color}, white 35%) 70%, transparent)`,
      );
    }
    return () => {
      root.style.removeProperty("--primary");
      root.style.removeProperty("--primary-foreground");
      root.style.removeProperty("--ring");
      root.style.removeProperty("--primary-light");
      root.style.removeProperty("--primary-light-soft");
    };
  }, [color]);
}
