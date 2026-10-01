import { Download, KeyRound, Rocket, UserPlus, Users } from "lucide-react";
import { Card } from "@shpyrd/ui/components/card";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { DeveloperStep } from "@/components/for-developers";

// For developers - your first deploy, on shpyrd cloud: sign up, sign the CLI
// in, deploy from a folder, share the result. Every command is in docs/cli,
// docs/deploying and docs/access. A laptop cluster is not here: that is "Run
// it yourself".

export const metadata = { title: "For developers · Your first deploy" };

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        variant="medium"
        heading="Your first deploy, on shpyrd cloud."
        description="Sign up, sign the CLI in, and deploy from the folder your app is in. There is no cluster to create."
      />

      <Timeline clip className="max-w-2xl">
        <TimelineItem icon={<UserPlus />}>
          <strong>Sign up.</strong> Your workspace answers at an address of its own, like{" "}
          <code className="font-mono text-foreground">acme.shpyrd.app</code>; your apps get theirs under it.
        </TimelineItem>
        <TimelineItem icon={<Download />}>
          <strong>Install the CLI.</strong>
          <IDE className="mt-2" code="brew install shpyrd-io/tap/shpyrd" language="sh" showLineNumbers={false} />
        </TimelineItem>
        <TimelineItem icon={<KeyRound />}>
          <strong>Sign it in.</strong> Create a token under Workspace › API tokens, then:
          <IDE
            className="mt-2"
            code="shpyrd login --url https://acme.shpyrd.app --token shp_…"
            language="sh"
            showLineNumbers={false}
          />
        </TimelineItem>
        <TimelineItem icon={<Rocket />} type="primary">
          <strong>Deploy from your folder.</strong> It is built (buildpacks, or your Dockerfile) and gets an address
          with TLS, with a sign-in in front: nobody gets in yet.
          <IDE
            className="mt-2"
            code={`shpyrd projects create "Purchase requests"\nshpyrd deploy\nshpyrd open   # https://purchase-requests.acme.shpyrd.app`}
            language="sh"
            showLineNumbers={false}
          />
        </TimelineItem>
        <TimelineItem icon={<Users />}>
          <strong>Share it.</strong> With a team, or with named people.
          <IDE
            className="mt-2"
            code={`shpyrd members add purchase-requests --team finance --role user\nshpyrd members add purchase-requests --user ana@acme.com --role user`}
            language="sh"
            showLineNumbers={false}
          />
        </TimelineItem>
      </Timeline>

      <Stack gap="spacious">
        <SectionIntro
          heading="Or ask your agent"
          description="These are ordinary commands, so the coding agent you work with can run them for you."
        />
        <Card className="max-w-2xl px-6 py-6">
          <Conversation>
            <ConversationMessage from="person" author="You">
              Deploy this to shpyrd as “Purchase requests” and let the Finance team use it.
            </ConversationMessage>
            <ConversationMessage
              from="agent"
              author="Your agent runs"
              steps={[
                { label: 'shpyrd projects create "Purchase requests"' },
                { label: "shpyrd deploy" },
                { label: "shpyrd members add purchase-requests --team finance --role user" },
              ]}
            >
              Done. Finance can open it at purchase-requests.acme.shpyrd.app with their work account.
            </ConversationMessage>
          </Conversation>
        </Card>
      </Stack>

      <DeveloperStep
        next={{ href: "/for/developers/whats-included", label: "What's included" }}
        action={{ href: "#", label: "Get started" }}
      />
    </PageLayoutContent>
  );
}
