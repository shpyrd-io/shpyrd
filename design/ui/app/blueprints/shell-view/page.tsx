"use client";

import { useRef, useState } from "react";
import { ShellView, type ShellStatus } from "@shpyrd/ui/components/shell-view";
import { Terminal, type TerminalHandle } from "@shpyrd/ui/components/terminal";
import { Section } from "../../section";

const instances = [
  { name: "web-1", ready: true },
  { name: "web-2", ready: true },
  { name: "worker-1", ready: false },
];

// A shell that is made up: it answers a few commands, in the terminal,
// so that the view can be seen working without a socket.
function useMadeUpShell(term: React.RefObject<TerminalHandle | null>) {
  const line = useRef("");
  const prompt = () => term.current?.write("\r\n\u001b[32mapp@web-1\u001b[0m:\u001b[34m~/app\u001b[0m$ ");
  const run = (command: string) => {
    const t = term.current;
    if (!t) return;
    switch (command.trim()) {
      case "":
        break;
      case "ls":
        t.write("\r\n\u001b[34mnode_modules\u001b[0m  package.json  server.js  \u001b[34mpublic\u001b[0m");
        break;
      case "pwd":
        t.write("\r\n/home/app/app");
        break;
      case "env":
        t.write("\r\nPORT=3000\r\nSHPYRD_PROCESS=web\r\nDATABASE_URL=postgres://…");
        break;
      case "clear":
        t.clear();
        return;
      case "help":
        t.write("\r\nThis shell is made up. It knows: ls, pwd, env, clear, help.");
        break;
      default:
        t.write(`\r\n\u001b[31mbash: ${command.trim()}: command not found\u001b[0m`);
    }
    prompt();
  };
  return {
    start: () => {
      term.current?.reset();
      term.current?.write("Connected to web-1. Type help.");
      prompt();
      term.current?.focus();
    },
    type: (data: string) => {
      const t = term.current;
      if (!t) return;
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

export default function Page() {
  const term = useRef<TerminalHandle>(null);
  const shell = useMadeUpShell(term);
  const [instance, setInstance] = useState("web-1");
  const [status, setStatus] = useState<ShellStatus>({ kind: "picking" });

  const alone = useRef<TerminalHandle>(null);
  const aloneShell = useMadeUpShell(alone);

  return (
    <>
      <Section title="A shell into an instance: choose, connect, type">
        <ShellView
          instances={instances}
          instance={instance}
          onInstanceChange={setInstance}
          status={status}
          onConnect={() => {
            setStatus({ kind: "connecting", instance });
            setTimeout(() => {
              setStatus({ kind: "open", instance, shell: "bash" });
              shell.start();
            }, 600);
          }}
          onDisconnect={() => {
            term.current?.write("\r\n\r\nSession ended.");
            setStatus({ kind: "closed", instance, message: "Session ended" });
          }}
          terminalRef={term}
          onData={shell.type}
          height={320}
        />
      </Section>
      <Section title="The terminal by itself, in the colours of the theme">
        <Terminal
          ref={alone}
          onData={aloneShell.type}
          height={200}
          onClick={() => {
            if (!alone.current) return;
            alone.current.focus();
          }}
        />
        <p className="text-xs text-muted-foreground">
          Click into it and type <code className="font-mono">help</code>. Switch the theme: it follows.
        </p>
      </Section>
    </>
  );
}
