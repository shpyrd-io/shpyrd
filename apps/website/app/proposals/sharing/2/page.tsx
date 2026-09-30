import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ConnectOnce, ProposalNav } from "@/components/proposals";

// How sharing works, proposal 2 - say it your way. Six things people say, six
// answers. The page teaches that there is nothing to learn: every one of these
// is how you would say it to a colleague.

export const metadata = { title: "How sharing works · Proposal 2" };

const exchanges = [
  { you: "Share it with Ana and João.", agent: "Done. They sign in with their work account and they're in." },
  { you: "Finance should have it too.", agent: "The whole Finance team can open it now, and whoever joins it later." },
  { you: "Let João make changes.", agent: "João can update it. Ana can still only use it." },
  { you: "The auditor just needs to look.", agent: "Rita can open it and read. She can't change anything." },
  { you: "Take Ana off.", agent: "Ana can't open it any more." },
  { you: "Who has it?", agent: "Finance, Ana and João. João can change it; you decide who's in." },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-12 py-8">
      <ProposalNav set="sharing" current={2} />

      <Hero
        align="center"
        heading="Say it the way you'd say it."
        description="Sharing is telling your agent who the app is for. There are no settings to learn."
      />

      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {exchanges.map((e) => (
          <li key={e.you}>
            <Card className="h-full px-5 py-5">
              <Conversation>
                <ConversationMessage from="person" author="You">
                  {e.you}
                </ConversationMessage>
                <ConversationMessage from="agent" author="Your agent">
                  {e.agent}
                </ConversationMessage>
              </Conversation>
            </Card>
          </li>
        ))}
      </ul>

      <ConnectOnce />
    </PageLayoutContent>
  );
}
