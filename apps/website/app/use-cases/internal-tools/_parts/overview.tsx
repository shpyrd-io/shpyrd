import Link from "next/link";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Launcher, launcherApps } from "@/components/proposals";
import { AgentWindow } from "@/components/agent-window";
import { pageSections } from "@/lib/page";

// Internal tools, the section's own page: the tool already works, and one
// sentence to your agent puts it where the team looks every morning. The
// kinds of tool, what shpyrd does and doesn't, and a week of one tracker are
// its subpages.

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>

      <Hero variant="page"
        label="Internal tools"
        heading="Built a tool for your team? Put it in their day."
        description="The tracker, the dashboard, the approval tool you built with AI. Tell your agent who it's for, and tomorrow it's in their list of apps."
        image={
          <BrowserFrame address="acme.shpyrd.app">
            <Launcher person="luis@acme.com" apps={launcherApps.slice(0, 4)} />
          </BrowserFrame>
        }
      />

      <AgentWindow>
        <Conversation>
          <ConversationMessage from="person" author="You">
            Share the purchase tracker with Finance and Operations.
          </ConversationMessage>
          <ConversationMessage from="agent" author="Your agent">
            Done. It&apos;s in their apps the next time they sign in with their work account.
            Nobody else can open it.
          </ConversationMessage>
        </Conversation>
      </AgentWindow>

      <p className="text-center text-sm text-muted-foreground">
        Works the same for trackers, dashboards, approval tools, quote tools and onboarding
        checklists.{" "}
        <Link href="#tools-people-build" className="font-medium text-foreground underline-offset-4 hover:underline">
          Find yours
        </Link>
      </p>


    </PageLayoutContent>
  );
}
