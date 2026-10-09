import { History, Rocket, Users } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { CodeWindow } from "@/components/code-window";
import { deployOutput } from "@/components/for-developers";
import { LoopSteps } from "@/components/loop-steps";
import { pageSections } from "@/lib/page";

// For developers - the overview: what shpyrd is for a developer. The deploy
// loop they remember from Heroku (push code, get a URL, roll back) on shpyrd
// cloud, with nothing to run. The rest has a tab each: the first deploy, what
// comes with it, and running it yourself.

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

export function Part() {
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
              <a href="#your-first-deploy">Your first deploy</a>
            </Button>
          </>
        }
        // The deploy's output, as it was, in a window like the home page's chat.
        image={<CodeWindow title="Terminal" detail="~/projects/shop" code={deployOutput} language="sh" />}
      />

      {/* The whole block, title and link too, in a soft glow of the page's
          own colour (white on the light page, black on the dark), so it
          reads over the moving binary mark behind it. */}
      <Stack gap="spacious" className="page-glow">
        <SectionIntro variant="xlarge" align="center"
          heading="The loop"
          link={
            <Button variant="link" asChild className="px-0">
              <a href="#whats-included">Everything that comes with it</a>
            </Button>
          }
        />
        {/* The three steps as the loop they are: deploy, share, take back,
            and round again (loop-steps.tsx). */}
        <LoopSteps className="mx-auto w-full max-w-5xl" steps={loop} />
      </Stack>


    </PageLayoutContent>
  );
}
