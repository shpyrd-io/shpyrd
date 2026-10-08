import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { AddToAgent } from "@/components/add-to-agent";
import { NextSubpage } from "@/components/use-case-agents-and-workers";
import { AgentWindow } from "@/components/agent-window";
import { BinaryMark } from "@/components/binary-mark";
import { pageSections } from "@/lib/page";

// Agents and workers · Ask your agent. One job: moving it off the laptop by
// asking the agent that built it, then checking on it. The agent's steps are
// real commands (docs/deploying, docs/logs).

export const metadata = { title: "Agents and workers · Move it off your laptop" };

export default function Page() {
  return (
    <>
    {/* The section's background: the shpyrd mark made of falling binary
        (binary-mark.tsx), as on its overview. */}
    {/* The same size and place on every page of the section: 847px tall,
        its top 100px down, its centre 370px right of the page's middle. */}
    <BinaryMark height={847} offset={100} x={370} />
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero align="center"
        variant="page"
        heading="Your agent runs on your laptop. Move it."
        description="Tell the agent that built it to put it on shpyrd cloud, where it keeps running. Ask it later how it's doing."
      />

      <AgentWindow data-binary-end>
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

      <NextSubpage
        action={<AddToAgent />}
        href="/use-cases/agents-and-workers/web-page-and-worker"
        title="Web page and worker"
      />
    </PageLayoutContent>
    </>
  );
}
