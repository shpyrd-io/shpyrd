import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ConnectOnce, ProposalNav } from "@/components/proposals";

// How sharing works, proposal 1 - one sentence. The whole page is the sentence
// you say to your agent and what it answers. Everything else is elsewhere.

export const metadata = { title: "How sharing works · Proposal 1" };

const more = [
  "Let João change it too.",
  "Take Ana off it.",
  "Who can open it?",
  "Give it to the whole company.",
];

export default function Page() {
  return (
    <PageLayoutContent width="medium" padding="normal" className="grid content-start gap-12 py-8">
      <ProposalNav set="sharing" current={1} />

      <Hero
        align="center"
        heading="Tell your agent who it's for."
        description="That's how sharing works. In the same conversation you built the app in."
      />

      <Card className="px-6 py-6">
        <Conversation>
          <ConversationMessage from="person" author="You">
            Share the purchase tracker with Ana, João and the Finance team.
          </ConversationMessage>
          <ConversationMessage from="agent" author="Your agent">
            Done. They can open it at <strong>purchases.acme.shpyrd.app</strong> with their work
            account. Nobody else can.
          </ConversationMessage>
        </Conversation>
      </Card>

      <div className="grid justify-items-center gap-3 text-center">
        <p className="text-sm text-muted-foreground">And later, just as easily:</p>
        <ul className="flex flex-wrap justify-center gap-2">
          {more.map((line) => (
            <li key={line} className="rounded-full bg-muted px-3.5 py-1.5 text-sm">
              {line}
            </li>
          ))}
        </ul>
      </div>

      <ConnectOnce />
    </PageLayoutContent>
  );
}
