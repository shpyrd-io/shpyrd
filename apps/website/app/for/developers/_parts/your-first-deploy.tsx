import { Download, KeyRound, Rocket, UserPlus, Users } from "lucide-react";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { VerticalRoute } from "@/components/vertical-route";
import { AgentWindow } from "@/components/agent-window";
import { pageSections } from "@/lib/page";

// For developers - your first deploy, on shpyrd cloud: sign up, sign the CLI
// in, deploy from a folder, share the result. Every command is in docs/cli,
// docs/deploying and docs/access. A laptop cluster is not here: that is "Run
// it yourself".

export function Part() {
  return (
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero as="h2" align="center"
        variant="page"
        heading="Your first deploy, on shpyrd cloud."
        description="Sign up, sign the CLI in, and deploy from the folder your app is in. There is no cluster to create."
      />

      {/* The steps as the home page's route, standing up: drawn as the
          reader reaches them. */}
      <VerticalRoute
        className="mx-auto w-full max-w-2xl"
        steps={[
          {
            icon: <UserPlus />,
            children: (
              <p>
                <strong>Sign up.</strong> Your workspace answers at an address of its own, like{" "}
                <code className="font-mono text-foreground">acme.shpyrd.app</code>; your apps get theirs under it.
              </p>
            ),
          },
          {
            icon: <Download />,
            children: (
              <>
                <p>
                  <strong>Install the CLI.</strong>
                </p>
                <IDE code="brew install shpyrd-io/tap/shpyrd" language="sh" showLineNumbers={false} />
              </>
            ),
          },
          {
            icon: <KeyRound />,
            children: (
              <>
                <p>
                  <strong>Sign it in.</strong> The browser opens your workspace; approve the code the terminal shows:
                </p>
                <IDE code="shpyrd login --url https://acme.shpyrd.app" language="sh" showLineNumbers={false} />
              </>
            ),
          },
          {
            icon: <Rocket />,
            lit: true,
            children: (
              <>
                <p>
                  <strong>Deploy from your folder.</strong> It is built (buildpacks, or your Dockerfile) and gets an
                  address with TLS, with a sign-in in front: nobody gets in yet.
                </p>
                <IDE
                  code={`shpyrd projects create "Purchase requests"\nshpyrd deploy\nshpyrd open   # https://purchase-requests.acme.shpyrd.app`}
                  language="sh"
                  showLineNumbers={false}
                />
              </>
            ),
          },
          {
            icon: <Users />,
            children: (
              <>
                <p>
                  <strong>Share it.</strong> With a team, or with named people.
                </p>
                <IDE
                  code={`shpyrd members add purchase-requests --team finance --role user\nshpyrd members add purchase-requests --user ana@acme.com --role user`}
                  language="sh"
                  showLineNumbers={false}
                />
              </>
            ),
          },
        ]}
      />

      <Stack gap="spacious">
        <SectionIntro align="center" variant="xlarge"
          heading="Or ask your agent"
          description="These are ordinary commands, so the coding agent you work with can run them for you."
        />
        <AgentWindow detail="~/projects/purchase-requests">
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
        </AgentWindow>
      </Stack>

    </PageLayoutContent>
  );
}
