import { Activity, History, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Closing, ProposalNav } from "@/components/proposals";

// Proposal 2 - ask your agent. Hypothesis H4: keeping the tools people already
// use removes the friction. The reader never leaves the conversation they built
// the app in; the page shows that conversation going one turn further.
//
// AHEAD OF THE PRODUCT: the agent deploying and sharing is the maintainer's bet
// (content/site/agents.ts). Today's MCP tools only read.

export const metadata = { title: "Proposal 2 · Ask your agent" };

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <ProposalNav current={2} />

      <Hero
        align="center"
        label="Works with Claude Code, Codex, Cursor and more"
        heading="Build it with your agent. Share it the same way."
        description="You already know how to ask for an app. Asking for your team to have it is one more sentence, in the same conversation."
        actions={
          <>
            <AddToAgent />
            <Button variant="outline" asChild>
              <a href={secondaryCta.href}>{secondaryCta.label}</a>
            </Button>
          </>
        }
      />

      <Card className="mx-auto w-full max-w-2xl px-6 py-6">
        <Conversation>
          <ConversationMessage from="person" author="You">
            The purchase tracker works. Put it online and let Finance and Operations use it.
            Marta should be able to change it too.
          </ConversationMessage>
          <ConversationMessage
            from="agent"
            author="Your agent · shpyrd workspace acme"
            steps={[
              { label: "Deployed purchase-requests, release 1" },
              { label: "purchases.acme.shpyrd.app is up, sign-in required" },
              { label: "Shared with Finance, Operations · can use" },
              { label: "Marta Silva · can update" },
            ]}
          >
            Done. Finance and Operations will find <strong>Purchase requests</strong> among
            their apps next time they sign in. Anyone else sees a sign-in page, not the app.
          </ConversationMessage>
          <ConversationMessage from="person" author="You">
            Someone in Finance says totals are wrong since this morning.
          </ConversationMessage>
          <ConversationMessage
            from="agent"
            author="Your agent · shpyrd workspace acme"
            steps={[
              { label: "Read the logs of release 3: 14 errors in /totals since 09:12" },
              { label: "Rolled back to release 2" },
            ]}
          >
            Release 3 broke the totals, so everyone is on release 2 again. Want me to look at
            the fix?
          </ConversationMessage>
        </Conversation>
      </Card>

      <Stack gap="spacious">
        <SectionIntro
          heading="What your agent can do in your workspace"
          description="Your agent works in a workspace your company runs. The address, the sign-in and the list of who is in stay put, whoever makes the change."
        />
        <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
          <Pillar icon={<Rocket />} heading="Deploy" description="Put an app online, with TLS and a sign-in in front of it." />
          <Pillar icon={<Users />} heading="Share" description="With a team or with named people, to use or to update." />
          <Pillar icon={<Activity />} heading="Watch" description="Read the logs and metrics when someone says it is slow." />
          <Pillar icon={<History />} heading="Roll back" description="Every change is a release. Go back to the one that worked." />
        </div>
      </Stack>

      <Closing />
    </PageLayoutContent>
  );
}
