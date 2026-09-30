"use client";

import * as React from "react";
import { cn } from "cn";
import { Button } from "./button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./select";
import { Terminal, type TerminalHandle } from "./terminal";

// A shell into a running instance: which instance, whether it is
// connected, and the terminal. Connecting is the application's: it is
// told to, and it writes into the terminal through the handle.

export type ShellInstance = { name: string; ready: boolean };

export type ShellStatus =
  | { kind: "picking" }
  | { kind: "connecting"; instance: string }
  | { kind: "open"; instance: string; shell: string }
  | { kind: "closed"; instance: string; message: string };

function ShellView({
  className,
  instances,
  instance,
  onInstanceChange,
  status,
  onConnect,
  onDisconnect,
  terminalRef,
  onData,
  onResize,
  height,
  hint = "One shell per project at a time; idle sessions close after 30 minutes.",
  ...props
}: Omit<React.ComponentProps<"div">, "onResize"> & {
  instances: ShellInstance[];
  instance?: string;
  onInstanceChange: (name: string) => void;
  status: ShellStatus;
  onConnect: () => void;
  onDisconnect: () => void;
  terminalRef?: React.Ref<TerminalHandle>;
  onData?: (data: string) => void;
  onResize?: (size: { cols: number; rows: number }) => void;
  height?: number;
  // What is said while nothing is connected.
  hint?: React.ReactNode;
}) {
  const live = status.kind === "open" || status.kind === "connecting";
  const said =
    status.kind === "open"
      ? `${status.instance} · ${status.shell}`
      : status.kind === "closed"
        ? status.message
        : status.kind === "connecting"
          ? `Connecting to ${status.instance}…`
          : instances.length === 0
            ? "No running instance"
            : hint;
  return (
    <div data-slot="shell-view" className={cn("grid gap-3", className)} {...props}>
      <div className="flex flex-wrap items-center gap-2">
        <Select value={instance ?? ""} onValueChange={onInstanceChange} disabled={live}>
          <SelectTrigger className="w-56">
            <SelectValue placeholder="Choose an instance" />
          </SelectTrigger>
          <SelectContent>
            {instances.map((i) => (
              <SelectItem key={i.name} value={i.name}>
                {i.name}
                {!i.ready && <span className="text-xs text-muted-foreground">not ready</span>}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {live ? (
          <Button variant="outline" onClick={onDisconnect}>
            Disconnect
          </Button>
        ) : (
          <Button onClick={onConnect} disabled={!instance}>
            {status.kind === "closed" ? "Reconnect" : "Connect"}
          </Button>
        )}
        <span data-slot="shell-status" className="text-xs text-muted-foreground">
          {said}
        </span>
      </div>
      <Terminal ref={terminalRef} onData={onData} onResize={onResize} height={height} />
    </div>
  );
}

export { ShellView };
