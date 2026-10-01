import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Stack } from "@shpyrd/ui/components/stack";
import { PurchaseApp } from "@/components/proposals";
import { InternalToolsNext } from "@/components/use-case-internal-tools";

// Internal tools · One tracker: the research's demo, told as three moments of
// one app's life: shared, handed a second maintainer, taken back after a bad
// change.

export const metadata = {
  title: "Internal tools · One week, one tracker",
  description: "One purchase tracker, built with AI: shared in one sentence, used by Finance, and put right in one more.",
};

const moments = [
  {
    when: "Monday",
    you: "Share the purchase tracker with Finance.",
    agent: "Done. Finance can open it with their work account.",
  },
  {
    when: "Wednesday",
    you: "Let Marta change it too.",
    agent: "Marta can update it now. Finance can still only use it.",
  },
  {
    when: "Friday",
    you: "Since this morning the totals are wrong. Put back yesterday's version.",
    agent: "Done. Everyone is on yesterday's version again.",
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        variant="medium"
        heading="One tracker, one week."
        description="Marta built a purchase tracker with AI in an afternoon. This is its first week, and all she said to her agent."
        image={
          <BrowserFrame address="purchases.acme.shpyrd.app">
            <PurchaseApp person="Luís" />
          </BrowserFrame>
        }
      />

      <ol className="grid gap-4 md:grid-cols-3">
        {moments.map((m) => (
          <li key={m.when}>
            <Stack gap="condensed">
              <p className="text-sm text-muted-foreground">{m.when}</p>
              <Card className="h-full px-5 py-5">
                <Conversation>
                  <ConversationMessage from="person" author="Marta">
                    {m.you}
                  </ConversationMessage>
                  <ConversationMessage from="agent" author="Her agent">
                    {m.agent}
                  </ConversationMessage>
                </Conversation>
              </Card>
            </Stack>
          </li>
        ))}
      </ol>

      <InternalToolsNext next={{ title: "Overview", href: "/use-cases/internal-tools" }} />
    </PageLayoutContent>
  );
}
