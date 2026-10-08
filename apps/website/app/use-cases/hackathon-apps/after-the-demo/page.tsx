import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { AddToAgent } from "@/components/add-to-agent";
import { HackathonStep } from "@/components/use-case-hackathon-apps";
import { AgentWindow } from "@/components/agent-window";
import { pageSections } from "@/lib/page";

// Apps from a hackathon - Monday morning. The gap between Friday's demo and
// Monday's use, closed in two sentences to the agent that built the apps.

export const metadata = { title: "Apps from a hackathon · After the demo" };

const turns = [
  {
    you: "Share the expense splitter with Finance.",
    agent: "Done. Finance can open it at expenses.acme.shpyrd.app with their work account.",
  },
  {
    you: "And the onboarding checklist with the People team and every manager.",
    agent: "Done. Nobody else can open either of them.",
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero align="center"
        variant="page"
        heading="Demo on Friday. In use on Monday."
        description="The people who clapped want to use it. Tell the agent that built it who it's for."
      />

      <AgentWindow>
        <Conversation>
          {turns.flatMap((t) => [
            <ConversationMessage key={`${t.you}-you`} from="person" author="You, on Monday">
              {t.you}
            </ConversationMessage>,
            <ConversationMessage key={`${t.you}-agent`} from="agent" author="Your agent">
              {t.agent}
            </ConversationMessage>,
          ])}
        </Conversation>
      </AgentWindow>

      <p className="mx-auto max-w-prose text-center text-muted-foreground">
        And if Tuesday&apos;s change breaks one of them, the version from Monday is a step away.
      </p>

      <HackathonStep
        action={<AddToAgent />}
        next={{ title: "Rollout checklist", href: "/use-cases/hackathon-apps/rollout-checklist" }}
      />
    </PageLayoutContent>
  );
}
