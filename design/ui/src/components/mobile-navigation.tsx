"use client";
import * as React from "react";
import { Menu, X } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";

type State = { open: boolean; setOpen: (open: boolean) => void; id: string; trigger: React.RefObject<HTMLButtonElement | null> };
const Context = React.createContext<State | null>(null);
function useNavigation() { const state = React.useContext(Context); if (!state) throw new Error("MobileNavigation parts require MobileNavigation"); return state; }
function MobileNavigation({ children, open: controlled, onOpenChange }: { children: React.ReactNode; open?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [local, setLocal] = React.useState(false);
  const trigger = React.useRef<HTMLButtonElement>(null);
  const id = React.useId();
  const setOpen = (value: boolean) => { setLocal(value); onOpenChange?.(value); };
  return <Context value={{ open: controlled ?? local, setOpen, id, trigger }}>{children}</Context>;
}
function MobileNavigationTrigger({ label = "Menu", closeLabel = "Close menu", ...props }: React.ComponentProps<typeof Button> & { label?: string; closeLabel?: string }) {
  const { open, setOpen, id, trigger } = useNavigation();
  return <Button {...props} ref={trigger} variant="ghost" size="icon" aria-label={open ? closeLabel : label} aria-expanded={open} aria-controls={id} onKeyDown={event => { props.onKeyDown?.(event); if (event.key === "Escape") setOpen(false); }} onClick={() => setOpen(!open)}>{open ? <X /> : <Menu />}</Button>;
}
function MobileNavigationContent({ className, children, ...props }: React.ComponentProps<"div">) {
  const { open, setOpen, id, trigger } = useNavigation();
  return <div {...props} id={id} hidden={!open} data-slot="mobile-navigation" className={cn("max-h-[calc(100dvh-8rem)] overflow-y-auto border-t p-3", className)} onKeyDown={event => { props.onKeyDown?.(event); if (event.key === "Escape") { setOpen(false); trigger.current?.focus(); } }} onClick={event => { props.onClick?.(event); if ((event.target as HTMLElement).closest("a[href]")) { setOpen(false); trigger.current?.focus(); } }}>{open ? children : null}</div>;
}
export { MobileNavigation, MobileNavigationTrigger, MobileNavigationContent };
