import Link from "next/link";
import { Card } from "@shpyrd/ui/components/card";
import { BrowserFrame } from "@shpyrd/ui/components/browser-frame";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ConnectOnce, Launcher, launcherApps } from "@/components/proposals";

// Internal tools, the section's own page: the tool already works, and one
// sentence to your agent puts it where the team looks every morning. The
// kinds of tool, what shpyrd does and doesn't, and a week of one tracker are
// its subpages.

export const metadata = {
  title: "Internal tools",
  description:
    "The tracker, the dashboard, the approval tool you built with AI. Tell your agent who it's for, and it's in their list of apps tomorrow morning.",
};

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">

      <Hero
        label="Internal tools"
        heading="Built a tool for your team? Put it in their day."
        description="The tracker, the dashboard, the approval tool you built with AI. Tell your agent who it's for, and tomorrow it's in their list of apps."
        image={
          <BrowserFrame address="acme.shpyrd.app">
            <Launcher person="luis@acme.com" apps={launcherApps.slice(0, 4)} />
          </BrowserFrame>
        }
      />

      <Card className="mx-auto w-full max-w-2xl px-6 py-6">
        <Conversation>
          <ConversationMessage from="person" author="You">
            Share the purchase tracker with Finance and Operations.
          </ConversationMessage>
          <ConversationMessage from="agent" author="Your agent">
            Done. It&apos;s in their apps the next time they sign in with their work account.
            Nobody else can open it.
          </ConversationMessage>
        </Conversation>
      </Card>

      <p className="text-center text-sm text-muted-foreground">
        Works the same for trackers, dashboards, approval tools, quote tools and onboarding
        checklists.{" "}
        <Link href="/use-cases/internal-tools/tools-people-build" className="font-medium text-foreground underline-offset-4 hover:underline">
          Find yours
        </Link>
      </p>

      <ConnectOnce />
    </PageLayoutContent>
  );
}
