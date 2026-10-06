"use client";

import { useEffect, useSyncExternalStore } from "react";

// Theme handling like the website's selector: light, dark or system. The
// choice is stored in localStorage; public/theme.js applies it before
// React renders to avoid a flash.

export type Theme = "light" | "dark" | "system";

const KEY = "shpyrd.theme";
let memoryTheme: Theme = "system";
const listeners = new Set<() => void>();

export function getTheme(): Theme {
  let v: string | null;
  try { v = localStorage.getItem(KEY); } catch { return memoryTheme; }
  return v === "light" || v === "dark" ? v : "system";
}

export function setTheme(t: Theme) {
  memoryTheme = t;
  try {
    if (t === "system") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, t);
  } catch { /* Keep the selection working when storage is unavailable. */ }
  applyTheme();
  for (const l of listeners) l();
}

export function resolvedTheme(): "light" | "dark" {
  const t = getTheme();
  if (t !== "system") return t;
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

export function applyTheme() {
  document.documentElement.classList.toggle("dark", resolvedTheme() === "dark");
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const theme = useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    getTheme,
    // A page may be rendered when the application is built, where there is
    // no browser: the choice is read once it reaches one.
    () => "system" as Theme,
  );
  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyTheme();
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return [theme, setTheme];
}
