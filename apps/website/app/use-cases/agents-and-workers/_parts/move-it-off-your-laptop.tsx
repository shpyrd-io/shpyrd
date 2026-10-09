import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { AgentWindow } from "@/components/agent-window";
import { pageSections } from "@/lib/page";

// Agents and workers · Ask your agent. One job: moving it off the laptop by
// asking the agent that built it, then checking on it. The agent's steps are
// real commands (docs/deploying, docs/logs).

export function Part() {
  return (
    <>
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2" align="center"
        variant="page"
        heading="Your agent runs on your laptop. Move it."
        description="Tell the agent that built it to put it on shpyrd cloud, where it keeps running. Ask it later how it's doing."
      />

      <AgentWindow>
        <Conversation>
          <ConversationMessage from="person" author="You">
            The inbox agent works. Put it on shpyrd so it runs without my laptop. The OpenAI key is
            in my .env.
          </ConversationMessage>
          <ConversationMessage
            from="agent"
            author="Your agent"
            steps={[
              { label: "Wrote shpyrd.yaml: one worker process, python agent.py" },
              { label: "shpyrd secrets set OPENAI_API_KEY=… (value not shown again)" },
              { label: "shpyrd deploy · release 1 · worker 1/1 running" },
            ]}
          >
            It&apos;s running. If it crashes it starts again on its own.
          </ConversationMessage>
          <ConversationMessage from="person" author="You">
            Is it still answering emails?
          </ConversationMessage>
          <ConversationMessage
            from="agent"
            author="Your agent"
            steps={[{ label: "shpyrd logs -p worker · last 200 lines" }]}
          >
            Yes: 38 replies since this morning, no errors.
          </ConversationMessage>
        </Conversation>
      </AgentWindow>

    </PageLayoutContent>
    </>
  );
}
