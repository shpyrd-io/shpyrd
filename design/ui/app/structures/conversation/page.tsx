import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="A request, and what the agent did about it">
        <Card className="max-w-2xl px-6 py-6">
          <Conversation>
            <ConversationMessage from="person" author="You">
              Put the purchase tracker online and let Finance use it.
            </ConversationMessage>
            <ConversationMessage
              from="agent"
              author="Your agent"
              steps={[
                { label: "Deployed purchase-requests, release 1" },
                { label: "Shared with Finance · can use" },
              ]}
            >
              Done. Finance will find it among their apps next time they sign in.
            </ConversationMessage>
          </Conversation>
        </Card>
      </Section>

      <Section title="Steps that are still running, or that failed">
        <Card className="max-w-2xl px-6 py-6">
          <Conversation>
            <ConversationMessage
              from="agent"
              author="Your agent"
              steps={[
                { label: "Built the image", status: "done" },
                { label: "Deploy of release 4 failed: port 3000 not answering", status: "failed" },
                { label: "Waiting for release 3 to take the traffic back", status: "pending" },
              ]}
            >
              Release 4 did not start, so release 3 stays up. Want me to read its logs?
            </ConversationMessage>
          </Conversation>
        </Card>
      </Section>

      <Section title="Turns with no steps, which is plain talk">
        <Card className="max-w-2xl px-6 py-6">
          <Conversation>
            <ConversationMessage from="person" author="You">
              Who can change the quote tool?
            </ConversationMessage>
            <ConversationMessage from="agent" author="Your agent">
              Sales ops can update it. Everyone in Sales can use it.
            </ConversationMessage>
          </Conversation>
        </Card>
      </Section>
    </>
  );
}
