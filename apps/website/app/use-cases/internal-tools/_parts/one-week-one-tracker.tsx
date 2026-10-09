import { Share2, Undo2, UserPen } from "lucide-react";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { PurchaseApp } from "@/components/proposals";
import { AgentWindow } from "@/components/agent-window";
import { VerticalRoute } from "@/components/vertical-route";
import { pageSections } from "@/lib/page";

// Internal tools · One tracker: the research's demo, told as three moments of
// one app's life: shared, handed a second maintainer, taken back after a bad
// change.

const moments = [
  {
    icon: <Share2 />,
    when: "Monday",
    you: "Share the purchase tracker with Finance.",
    agent: "Done. Finance can open it with their work account.",
  },
  {
    icon: <UserPen />,
    when: "Wednesday",
    you: "Let Marta change it too.",
    agent: "Marta can update it now. Finance can still only use it.",
  },
  {
    icon: <Undo2 />,
    lit: true,
    when: "Friday",
    you: "Since this morning the totals are wrong. Put back yesterday's version.",
    agent: "Done. Everyone is on yesterday's version again.",
  },
];

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero as="h2"
        variant="page"
        heading="One tracker, one week."
        description="Marta built a purchase tracker with AI in an afternoon. This is its first week, and all she said to her agent."
        image={
          <BrowserFrame address="purchases.acme.shpyrd.app">
            <PurchaseApp person="Luís" />
          </BrowserFrame>
        }
      />

      {/* The week as the home page's route, standing up (vertical-route.tsx):
          each day, what Marta said, in her agent's window. */}
      <VerticalRoute
        className="mx-auto w-full max-w-2xl"
        steps={moments.map((m) => ({
          icon: m.icon,
          lit: m.lit,
          children: (
            <>
              <p>
                <strong>{m.when}</strong>
              </p>
              <AgentWindow>
                <Conversation>
                  <ConversationMessage from="person" author="Marta">
                    {m.you}
                  </ConversationMessage>
                  <ConversationMessage from="agent" author="Her agent">
                    {m.agent}
                  </ConversationMessage>
                </Conversation>
              </AgentWindow>
            </>
          ),
        }))}
      />

    </PageLayoutContent>
  );
}
