import { ArrowUp, Sparkles } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Section } from "../../section";

const composer = (
  <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2 text-sm text-muted-foreground">
    <span className="flex-1">Message your agent…</span>
    <span className="grid size-6 place-items-center rounded-md bg-muted">
      <ArrowUp className="size-3.5" />
    </span>
  </div>
);

export default function Page() {
  return (
    <>
      <Section title="An app with its icon and name, what it has open, and a box at the bottom">
        <AppWindow
          title={
            <>
              <Sparkles />
              Your agent
            </>
          }
          detail="~/projects/crm"
          footer={composer}
          className="max-w-lg"
        >
          <div className="px-5 py-5">
            <Conversation>
              <ConversationMessage from="person" author="You">
                Put it on the internet.
              </ConversationMessage>
              <ConversationMessage from="agent" author="Your agent" steps={[{ label: "shpyrd deploy · release 1" }]}>
                Done. It&apos;s at crm.acme.shpyrd.app.
              </ConversationMessage>
            </Conversation>
          </div>
        </AppWindow>
      </Section>

      <Section title="Only a name; a long detail is cut">
        <AppWindow title="Notes" detail="Meeting with the Finance team about the purchase tracker" className="max-w-xs">
          <p className="px-5 py-4 text-sm text-muted-foreground">What the app shows.</p>
        </AppWindow>
      </Section>
    </>
  );
}
