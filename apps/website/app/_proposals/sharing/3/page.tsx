import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ConnectOnce, DeniedScreen, ProposalNav, PurchaseApp, SignInScreen } from "@/components/proposals";

// How sharing works, proposal 3 - you say, they see. Three beats, each one line
// said to the agent on the left and what the other person gets on the right.
// The colleague's side is the proof that the sentence did something.

export const metadata = { title: "How sharing works · Proposal 3" };

const beats = [
  {
    you: "Share the purchase tracker with Ana and the Finance team.",
    label: "Ana opens the link",
    screen: <SignInScreen app="Purchase requests" audience="Only the people it's shared with" />,
  },
  {
    you: null,
    label: "…signs in with her work account, and she's in",
    screen: <PurchaseApp person="Ana" />,
  },
  {
    you: null,
    label: "Pedro, who wasn't on your list, isn't",
    screen: <DeniedScreen app="Purchase requests" team="finance" />,
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-12 py-8">
      <ProposalNav set="sharing" current={3} />

      <Hero
        align="center"
        heading="You say it. They're in."
        description="One sentence to your agent. Here is what it looks like from the other side."
      />

      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] lg:items-start">
        <Card className="px-6 py-6 lg:sticky lg:top-24">
          <Conversation>
            <ConversationMessage from="person" author="You">
              {beats[0].you}
            </ConversationMessage>
            <ConversationMessage from="agent" author="Your agent">
              Done. Send them <strong>purchases.acme.shpyrd.app</strong>; they sign in with
              their work account.
            </ConversationMessage>
          </Conversation>
        </Card>

        <ol className="grid gap-8">
          {beats.map((beat) => (
            <li key={beat.label} className="grid gap-2">
              <p className="text-sm text-muted-foreground">{beat.label}</p>
              <BrowserFrame address="purchases.acme.shpyrd.app">{beat.screen}</BrowserFrame>
            </li>
          ))}
        </ol>
      </div>

      <ConnectOnce />
    </PageLayoutContent>
  );
}
