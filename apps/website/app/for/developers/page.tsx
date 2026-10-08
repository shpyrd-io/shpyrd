import { History, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { CodeWindow } from "@/components/code-window";
import { DeveloperNext, deployOutput } from "@/components/for-developers";
import { SubpageStep } from "@/components/subpage-step";
import { pageSections } from "@/lib/page";

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
    <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
      <Hero variant="page"
        label="For developers"
        heading="Push code. Get a URL. We run the cluster."
        description="The deploy loop you remember from Heroku, on shpyrd cloud: a workspace at your-team.shpyrd.app, with nothing to install and nothing to operate."
        actions={
          <>
            <Button size="lg" asChild>
              <a href="#">Get started</a>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <a href="/for/developers/your-first-deploy">Your first deploy</a>
            </Button>
          </>
        }
        // The deploy's output, as it was, in a window like the home page's chat.
        image={<CodeWindow title="Terminal" detail="~/projects/shop" code={deployOutput} language="sh" />}
      />

      <Stack gap="spacious">
        <SectionIntro variant="xlarge" align="center"
          heading="The loop"
          link={
            <Button variant="link" asChild className="px-0">
              <a href="/for/developers/whats-included">Everything that comes with it</a>
            </Button>
          }
        />
        <div className="grid gap-8 sm:grid-cols-3">
          {loop.map((l) => (
            <Pillar variant="card" key={l.heading} icon={l.icon} heading={l.heading} description={l.body} />
          ))}
        </div>
      </Stack>

      <DeveloperNext />

      {/* As every page of the section ends: the way to its next one. */}
      <SubpageStep next={{ href: "/for/developers/your-first-deploy", title: "Your first deploy" }} />
    </PageLayoutContent>
  );
}
