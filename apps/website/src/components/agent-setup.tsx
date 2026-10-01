"use client";

import { IDE } from "@shpyrd/ui/components/ide";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { addToAgent, agents } from "@shpyrd/content/site/agents";
import { AgentMark } from "@/components/add-to-agent";

// How to add shpyrd to each agent, one tab each: pick yours, and the panel
// says what to paste and where. The snippets are the ones the "Add to" button
// shows (content/site/agents.ts), so the two never disagree.
const languageOf = (file?: string) =>
  file?.endsWith(".toml") ? "toml" : file?.endsWith(".json") ? "json" : "sh";

export function AgentSetup() {
  return (
    <Tabs defaultValue={agents[0].id} className="not-prose my-6 min-w-0">
      {/* Five names do not fit a narrow column: the list scrolls sideways,
          with no bar drawn. */}
      <div className="-m-1 overflow-x-auto overflow-y-hidden p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <TabsList>
          {agents.map((agent) => (
            <TabsTrigger key={agent.id} value={agent.id} icon={<AgentMark id={agent.id} />}>
              {agent.name}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      {agents.map((agent) => (
        <TabsContent key={agent.id} value={agent.id} className="grid gap-2 pt-3">
          <p className="text-sm text-muted-foreground">
            {agent.kind === "command"
              ? "In a terminal:"
              : agent.kind === "steps"
                ? "In Claude:"
                : `In ${agent.file}:`}
          </p>
          <IDE
            code={agent.snippet}
            language={agent.kind === "steps" ? "txt" : languageOf(agent.file)}
            showLineNumbers={false}
          />
        </TabsContent>
      ))}
      <p className="mt-3 text-sm text-muted-foreground">{addToAgent.note}</p>
    </Tabs>
  );
}
