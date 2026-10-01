import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { AddToAgent } from "@/components/add-to-agent";
import { HackathonStep } from "@/components/use-case-hackathon-apps";

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
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-12 py-8">
      <Hero
        variant="medium"
        heading="Demo on Friday. In use on Monday."
        description="The people who clapped want to use it. Tell the agent that built it who it's for."
      />

      <Card className="w-full max-w-2xl px-6 py-6">
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
      </Card>

      <p className="max-w-prose text-muted-foreground">
        And if Tuesday&apos;s change breaks one of them, the version from Monday is a step away.
      </p>

      <HackathonStep
        action={<AddToAgent />}
        next={{ title: "Rollout checklist", href: "/use-cases/hackathon-apps/rollout-checklist" }}
      />
    </PageLayoutContent>
  );
}
