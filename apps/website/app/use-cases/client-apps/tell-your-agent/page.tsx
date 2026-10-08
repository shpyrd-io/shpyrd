import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ClientAppsStep } from "@/components/use-case-client-apps";
import { AgentWindow } from "@/components/agent-window";
import { pageSections } from "@/lib/page";

// Client apps · Tell your agent: for whoever builds the client's app with
// Claude Code or Codex, the delivery is two sentences in the same
// conversation, one at go-live and one at the handover. The agent runs
// ordinary shpyrd commands (members add, members remove); it needs no tools
// beyond the CLI.

export const metadata = {
  title: "Client apps · Tell your agent",
  description:
    "Built a client's app with your agent? Tell it who at the client should have it, and later, that it's theirs now.",
};

export default function Page() {
  return (
    <PageLayoutContent width="medium" padding="normal" className={pageSections}>

      <Hero
        align="center"
        variant="page"
        heading="Built it for a client? Tell your agent who's theirs."
        description="Two sentences, weeks apart. One when their team starts using it, one when you hand it over. On shpyrd cloud or in their own cloud."
      />

      <AgentWindow>
        <Conversation>
          <ConversationMessage from="person" author="You · go-live">
            Give the order portal to Northwind&apos;s Operations team. Our team keeps the right
            to change it.
          </ConversationMessage>
          <ConversationMessage from="agent" author="Your agent">
            Done. Operations can open it with their Northwind account. Your team can still
            deploy.
          </ConversationMessage>
          <ConversationMessage from="person" author="You · handover">
            It&apos;s theirs now. Give Northwind&apos;s IT the keys and take our team off.
          </ConversationMessage>
          <ConversationMessage from="agent" author="Your agent">
            Done. Northwind&apos;s IT decides who&apos;s in from here. Your team can&apos;t open
            it any more.
          </ConversationMessage>
        </Conversation>
      </AgentWindow>

      <ClientAppsStep next={{ href: "/use-cases/client-apps", label: "Overview" }} />
    </PageLayoutContent>
  );
}
