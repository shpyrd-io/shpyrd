import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { AddToAgent } from "@/components/add-to-agent";
import { NextSubpage } from "@/components/use-case-agents-and-workers";

// Agents and workers · Ask your agent. One job: moving it off the laptop by
// asking the agent that built it, then checking on it. The agent's steps are
// real commands (docs/deploying, docs/logs).

export const metadata = { title: "Agents and workers · Move it off your laptop" };

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="Your agent runs on your laptop. Move it."
        description="Tell the agent that built it to put it on shpyrd cloud, where it keeps running. Ask it later how it's doing."
      />

      <Card className="w-full max-w-2xl px-6 py-6">
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
      </Card>

      <NextSubpage
        action={<AddToAgent />}
        href="/use-cases/agents-and-workers/web-page-and-worker"
        title="Web page and worker"
      />
    </PageLayoutContent>
  );
}
