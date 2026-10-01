import { History, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { IDE } from "@shpyrd/ui/components/ide";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { DeveloperNext, deployOutput } from "@/components/for-developers";

// For developers - the overview: what shpyrd is for a developer. The deploy
// loop they remember from Heroku (push code, get a URL, roll back) on shpyrd
// cloud, with nothing to run. The rest has a tab each: the first deploy, what
// comes with it, and running it yourself.

export const metadata = {
  title: "For developers",
  description:
    "Deploy from your folder, get a URL with TLS, roll back in one command. On shpyrd cloud, with nothing to install or run.",
};

const loop = [
  {
    icon: <Rocket />,
    heading: "Deploy from your folder",
    body: "No manifests and no YAML to write. Buildpacks work out the language, or your Dockerfile is used.",
  },
  {
    icon: <Users />,
    heading: "Share it by name",
    body: "Every app gets an address with TLS and a sign-in in front. You say who gets in: a team, or a person.",
  },
  {
    icon: <History />,
    heading: "Take a change back",
    body: "Every deploy and config change is a numbered release. Rolling back restores the build and its config.",
  },
];

export default function Page() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <Hero
        label="For developers"
        heading="Push code. Get a URL. We run the cluster."
        description="The deploy loop you remember from Heroku, on shpyrd cloud: a workspace at your-team.shpyrd.app, with nothing to install and nothing to operate."
        actions={
          <>
            <Button asChild>
              <a href="#">Get started</a>
            </Button>
            <Button variant="outline" asChild>
              <a href="/for/developers/your-first-deploy">Your first deploy</a>
            </Button>
          </>
        }
        image={<IDE code={deployOutput} language="sh" showLineNumbers={false} />}
      />

      <Stack gap="spacious">
        <SectionIntro
          heading="The loop"
          link={
            <Button variant="link" asChild className="px-0">
              <a href="/for/developers/whats-included">Everything that comes with it</a>
            </Button>
          }
        />
        <div className="grid gap-8 sm:grid-cols-3">
          {loop.map((l) => (
            <Pillar key={l.heading} icon={l.icon} heading={l.heading} description={l.body} />
          ))}
        </div>
      </Stack>

      <DeveloperNext />
    </PageLayoutContent>
  );
}
