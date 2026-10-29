import { Globe, Rocket, Sparkles } from "lucide-react";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Hero } from "@shpyrd/ui/components/hero";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { Chat, Message } from "@/components/agent-choice";
import { Boundaries } from "@/components/boundaries";
import { buildExample, closing, intro, steps } from "@/content/sharing";
import { ContainerCircle } from "@/components/container-helix";
import { VerticalRoute } from "@/components/vertical-route";
import { pageSections } from "@/lib/page";
import { signUp } from "@/lib/signup";
import { ScrollReveal } from "@/components/scroll-reveal";

export const metadata = { title: "Getting Started", description: intro.lead };

const icons: Record<string, React.ReactElement> = {
  build: <Sparkles />,
  publish: <Rocket />,
  share: <Globe />,
};

export default function GettingStarted() {
  return (
    <>
      {/* The page's blocks rise in as they come into view, as on the home. */}
      <ScrollReveal />
      <ContainerCircle />
      <PageLayoutContent width="xlarge" padding="normal" className={pageSections}>
        <Hero variant="page" align="center" heading={intro.title} description={intro.lead} />

        <VerticalRoute
          className="mx-auto w-full max-w-5xl"
          flush
          steps={steps.map((step, index) => ({
            icon: icons[step.id],
            lit: true,
            children: (
              <div className={cn(glass, "grid gap-6 p-6 pt-[18px] sm:p-8 sm:pt-[18px] lg:grid-cols-2 lg:items-start lg:gap-12 shadow-none dark:shadow-none")}>
                <Stack gap="normal">
                  <p className="text-sm leading-5 font-medium text-primary">Step {index + 1}</p>
                  <h2 className="text-2xl font-semibold text-foreground">{step.title}</h2>
                  <p className="max-w-prose text-muted-foreground">{step.body}</p>
                  <Boundaries items={[{
                    id: step.id,
                    step: step.id,
                    claim: step.note.title,
                    limit: step.note.body,
                  }]} />
                </Stack>
                <div className="min-w-0 lg:pt-[14px] [&>div]:my-0">
                  {step.id === "build" ? (
                    <Chat detail="Build your idea">
                      <Message from="person">{buildExample.prompt}</Message>
                      <Message from="agent">{buildExample.reply}</Message>
                    </Chat>
                  ) : (
                    <img
                      src={step.id === "publish" ? "/screenshots/deploy-dialog-crop.png" : "/screenshots/launcher.png"}
                      alt={step.id === "publish" ? "The shpyrd deployment dialog with a repository ready to deploy" : "The shpyrd launcher showing the apps available to a signed-in user"}
                      loading="lazy"
                      className="w-full rounded-2xl border border-white/80 shadow-[0_6px_16px_rgb(25_25_40/0.07)] dark:border-white/10 dark:invert dark:hue-rotate-180"
                    />
                  )}
                </div>
              </div>
            ),
          }))}
        />

        <div className="grid justify-items-center gap-6 text-center">
          <SectionIntro align="center" variant="xlarge" className="w-full" heading={closing.title} />
          <Button size="lg" asChild>
            <a href={signUp}>{closing.action}</a>
          </Button>
        </div>
      </PageLayoutContent>
    </>
  );
}
