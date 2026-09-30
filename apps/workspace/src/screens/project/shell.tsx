"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShellView, type ShellStatus } from "@shpyrd/ui/components/shell-view";
import type { TerminalHandle } from "@shpyrd/ui/components/terminal";
import { api, ApiError } from "@/api/api";
import type { Project } from "@/api/types";

// What the server says over the socket, besides the bytes of the shell.
type Control = { type: "open"; instance: string; shell: string } | { type: "exit"; code: number } | { type: "error"; message: string };

// A shell into a running instance: a ticket from the server, then a
// socket it bridges to the instance. The Mock has no socket: it answers
// with a shell that is made up.
export function Shell({ project }: { project: Project }) {
  const term = useRef<TerminalHandle>(null);
  const socket = useRef<WebSocket | null>(null);
  const instances = useQuery({ queryKey: ["instances", project.slug], queryFn: () => api.instances(project.slug), refetchInterval: 15_000 });
  const [instance, setInstance] = useState("");
  const [status, setStatus] = useState<ShellStatus>({ kind: "picking" });
  useEffect(() => {
    if (!instance && instances.data?.length) setInstance((instances.data.find((i) => i.ready) ?? instances.data[0]!).name);
  }, [instances.data, instance]);
  useEffect(() => () => socket.current?.close(), []);

  const pretend = usePretendShell(term, instance, status, setStatus);

  const connect = useCallback(async () => {
    const t = term.current;
    if (!t || !instance) return;
    socket.current?.close();
    t.reset();
    setStatus({ kind: "connecting", instance });
    t.writeln(`Connecting to ${instance}…`);
    // A quick reconnect can outrun the server letting the last session
    // go: one more try covers it without hiding a real conflict.
    const mint = async () => {
      try {
        return await api.shellTicket(project.slug, instance);
      } catch (e) {
        if (!(e instanceof ApiError) || e.status !== 409) throw e;
        await new Promise((done) => setTimeout(done, 500));
        return await api.shellTicket(project.slug, instance);
      }
    };
    let url: string | null;
    try {
      const { ticket } = await mint();
      url = await api.shellSocket(project.slug, instance, ticket);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      t.writeln(`\r\n${message}`);
      setStatus({ kind: "closed", instance, message });
      return;
    }
    if (url === null) return pretend.open();
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    socket.current = sock;
    sock.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        let control: Control;
        try {
          control = JSON.parse(ev.data) as Control;
        } catch {
          return;
        }
        if (control.type === "open") {
          setStatus({ kind: "open", instance: control.instance, shell: control.shell });
          sock.send(JSON.stringify({ type: "resize", cols: t.cols, rows: t.rows }));
          t.focus();
        } else if (control.type === "exit") {
          setStatus({ kind: "closed", instance, message: `The shell ended with status ${control.code}` });
        } else if (control.type === "error") {
          t.writeln(`\r\n${control.message}`);
          setStatus({ kind: "closed", instance, message: control.message });
        }
        return;
      }
      // Bytes, so what a program prints stays as it was.
      t.write(new Uint8Array(ev.data as ArrayBuffer));
    };
    sock.onclose = () => {
      if (socket.current === sock) socket.current = null;
      setStatus((prev) => (prev.kind === "closed" ? prev : { kind: "closed", instance, message: "Session ended" }));
    };
    sock.onerror = () => t.writeln("\r\nThe connection failed.");
  }, [instance, pretend, project.slug]);

  const encoder = useRef(new TextEncoder());
  return (
    <ShellView
      instances={(instances.data ?? []).map((i) => ({ name: i.name, ready: i.ready }))}
      instance={instance}
      onInstanceChange={setInstance}
      status={status}
      onConnect={connect}
      onDisconnect={() => {
        if (socket.current) socket.current.close();
        else pretend.close();
      }}
      terminalRef={term}
      onData={(data) => {
        const sock = socket.current;
        if (sock?.readyState === WebSocket.OPEN) sock.send(encoder.current.encode(data));
        else pretend.type(data);
      }}
      onResize={({ cols, rows }) => {
        const sock = socket.current;
        if (sock?.readyState === WebSocket.OPEN) sock.send(JSON.stringify({ type: "resize", cols, rows }));
      }}
      height={520}
    />
  );
}

// The shell of the Mock: a prompt, a few commands, nothing behind them.
function usePretendShell(term: React.RefObject<TerminalHandle | null>, instance: string, status: ShellStatus, setStatus: (s: ShellStatus) => void) {
  const line = useRef("");
  const prompt = () => term.current?.write(`\r\n\u001b[32mapp@${instance}\u001b[0m:\u001b[34m~/app\u001b[0m$ `);
  const run = (command: string) => {
    const t = term.current;
    if (!t) return;
    switch (command.trim()) {
      case "":
        break;
      case "ls":
        t.write("\r\n\u001b[34mnode_modules\u001b[0m  package.json  server.js  \u001b[34mpublic\u001b[0m");
        break;
      case "env":
        t.write(`\r\nPORT=3000\r\nSHPYRD_PROCESS=${instance.split("-")[0]}\r\nDATABASE_URL=postgres://…`);
        break;
      case "clear":
        t.clear();
        return;
      case "exit":
        t.write("\r\n\r\nSession ended.");
        setStatus({ kind: "closed", instance, message: "The shell ended with status 0" });
        return;
      default:
        t.write(`\r\n\u001b[31mbash: ${command.trim()}: command not found\u001b[0m`);
    }
    prompt();
  };
  return {
    open: () => {
      setTimeout(() => {
        setStatus({ kind: "open", instance, shell: "bash" });
        term.current?.reset();
        term.current?.write(`Connected to ${instance}.`);
        prompt();
        term.current?.focus();
      }, 600);
    },
    close: () => {
      term.current?.write("\r\n\r\nSession ended.");
      setStatus({ kind: "closed", instance, message: "Session ended" });
    },
    type: (data: string) => {
      const t = term.current;
      if (!t || status.kind !== "open") return;
      for (const ch of data) {
        if (ch === "\r") {
          const command = line.current;
          line.current = "";
          run(command);
        } else if (ch === "\u007f") {
          if (line.current.length > 0) {
            line.current = line.current.slice(0, -1);
            t.write("\b \b");
          }
        } else if (ch >= " ") {
          line.current += ch;
          t.write(ch);
        }
      }
    },
  };
}
