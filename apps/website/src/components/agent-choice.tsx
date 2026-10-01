"use client";

import { useEffect, useSyncExternalStore } from "react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { IDE } from "@shpyrd/ui/components/ide";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { addToAgent, agents } from "@shpyrd/content/site/agents";
import { AgentMark } from "@/components/add-to-agent";

// The agent the reader said they use. Picking one in the setup tabs makes it
// the agent of every conversation on the page, and it is remembered in this
// browser for the next page. Until then (and where the page is built) it is
// the first: Claude Code.
const KEY = "shpyrd.agent";
const first = agents[0].id;
let chosen = first;
const listeners = new Set<() => void>();

const store = {
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => void listeners.delete(listener);
  },
  now: () => chosen,
  atStart: () => first,
  choose(id: string) {
    if (id === chosen || !agents.some((a) => a.id === id)) return;
    chosen = id;
    try {
      localStorage.setItem(KEY, id);
    } catch {
      // Storage refused: the choice still holds for this page.
    }
    listeners.forEach((l) => l());
  },
};

function useAgent() {
  const id = useSyncExternalStore(store.subscribe, store.now, store.atStart);
  // What this browser remembered, read once the page is in it.
  useEffect(() => {
    try {
      const saved = localStorage.getItem(KEY);
      if (saved) store.choose(saved);
    } catch {
      // No storage: the first agent it is.
    }
  }, []);
  return agents.find((a) => a.id === id) ?? agents[0];
}

const languageOf = (file?: string) =>
  file?.endsWith(".toml") ? "toml" : file?.endsWith(".json") ? "json" : "sh";

// How to add shpyrd to each agent, one tab each: pick yours, and the panel
// says what to paste and where. The snippets are the ones the "Add to" button
// shows (content/site/agents.ts), so the two never disagree.
export function AgentSetup() {
  const agent = useAgent();
  return (
    <Tabs value={agent.id} onValueChange={store.choose} className="not-prose my-6 min-w-0">
      {/* Five names do not fit a narrow column: the list scrolls sideways,
          with no bar drawn. */}
      <div className="-m-1 overflow-x-auto overflow-y-hidden p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <TabsList>
          {agents.map((a) => (
            <TabsTrigger key={a.id} value={a.id} icon={<AgentMark id={a.id} />}>
              {a.name}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      {agents.map((a) => (
        <TabsContent key={a.id} value={a.id} className="grid gap-2 pt-3">
          <p className="text-sm text-muted-foreground">
            {a.kind === "command" ? "In a terminal:" : a.kind === "steps" ? "In Claude:" : `In ${a.file}:`}
          </p>
          <IDE code={a.snippet} language={a.kind === "steps" ? "txt" : languageOf(a.file)} showLineNumbers={false} />
        </TabsContent>
      ))}
      <p className="mt-3 text-sm text-muted-foreground">{addToAgent.note}</p>
    </Tabs>
  );
}

// The chosen agent, with its mark: the name on a window and on its turns.
function AgentName({ size = "size-3.5" }: { size?: string }) {
  const agent = useAgent();
  return (
    <span key={agent.id} className="inline-flex items-center gap-1.5 animate-in fade-in-0 duration-normal ease-enter">
      <AgentMark id={agent.id} className={size} />
      {agent.name}
    </span>
  );
}

// A conversation with an agent, in the agent's window: what was asked, what
// it did, what it answered. The agent is the one the reader picked.
export function Chat({ detail, children }: { detail?: string; children: React.ReactNode }) {
  return (
    <AppWindow title={<AgentName />} detail={detail} className="not-prose my-6">
      <div className="px-5 py-5">
        <Conversation>{children}</Conversation>
      </div>
    </AppWindow>
  );
}

export function Message({
  from,
  author,
  steps,
  children,
}: {
  from: "person" | "agent";
  author?: string;
  steps?: string[];
  children: React.ReactNode;
}) {
  return (
    <ConversationMessage
      from={from}
      author={author ?? (from === "person" ? "You" : <AgentName size="size-3" />)}
      steps={steps?.map((label) => ({ label }))}
      className="[&_p]:m-0"
    >
      {children}
    </ConversationMessage>
  );
}
