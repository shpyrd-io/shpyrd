import { ChevronRight, FileCode } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Checklist } from "@shpyrd/ui/components/checklist";
import { Conversation, ConversationMessage } from "@shpyrd/ui/components/conversation";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { contact } from "@shpyrd/content/site/offer";
import type { Landing, Link } from "@shpyrd/content/site/landings";
import { AgentWindow } from "@/components/agent-window";
import { BinaryMark } from "@/components/binary-mark";
import { CodeWindow } from "@/components/code-window";
import { ContainerCircle } from "@/components/container-helix";
import { Faq } from "@/components/faq";
import { deployOutput } from "@/components/for-developers";
import { icons } from "@/components/landing-icons";
import { LoopSteps } from "@/components/loop-steps";
import { Launcher, SignInScreen, launcherApps } from "@/components/proposals";
import { Screen } from "@/components/screen";
import { ShipyardCta } from "@/components/shipyard-cta";
import { SimpleCta } from "@/components/simple-cta";
import { webAndWorkerYaml, workerYaml } from "@/components/use-case-agents-and-workers";
import { VerticalRoute } from "@/components/vertical-route";
import { designs, plain, type Design } from "@/lib/landing-designs";
import { signUp } from "@/lib/signup";
import { pageSections } from "@/lib/page";

// A feature or solution page (content/site/features.ts, solutions.ts). The
// words are the content's; how each page is drawn is its design
// (src/lib/landing-designs.ts), chosen from the site's and design/ui's own
// blocks so that each page shows its message: the binary ship or the circle of
// containers behind the top; the words alone or beside a picture of the
// product; the example as a loop, a route, three steps, three options or a
// conversation with an agent; the capabilities as cards, a grid or a
// checklist; the questions to open or side by side; the simple CTA or the
// shipyard to end.

// Where a link goes: a workspace on shpyrd cloud, the conversation, or a page.
const to = (l: Link) => (l.href === "signup" ? signUp : l.href === "contact" ? contact.href : l.href);

type Step = Landing["example"]["steps"][number];
type Item = Landing["capabilities"]["items"][number];

// A picture of the product, from the site's own: the deploy in a terminal,
// the file that declares an app, the launcher, the sign-in, a site.
function Picture({ kind, className }: { kind: NonNullable<Design["picture"]>; className?: string }) {
  switch (kind) {
    case "terminal":
      return <CodeWindow title="Terminal" detail="~/projects/shop" code={deployOutput} language="sh" roomy className={className} />;
    case "yaml":
      return <CodeWindow title="shpyrd.yaml" icon={<FileCode />} code={webAndWorkerYaml} language="yaml" roomy className={className} />;
    case "worker":
      return <CodeWindow title="shpyrd.yaml" icon={<FileCode />} code={workerYaml} language="yaml" roomy className={className} />;
    case "launcher":
      return (
        <Screen address="acme.shpyrd.app" className={className}>
          <Launcher person="luis@acme.com" apps={launcherApps.slice(0, 4)} />
        </Screen>
      );
    case "signin":
      return (
        <Screen address="purchases.acme.shpyrd.app" className={className}>
          <SignInScreen app="Purchase requests" audience="Finance and Operations" />
        </Screen>
      );
    case "browser":
      // A site, drawn without words: its bar, its opening, three blocks.
      return (
        <Screen address="acme.com" className={className}>
          <div aria-hidden="true" className="grid gap-6 bg-background p-6">
            <div className="flex items-center justify-between">
              <Skeleton className="h-4 w-20" />
              <div className="flex gap-3">
                <Skeleton className="h-3 w-10" />
                <Skeleton className="h-3 w-10" />
                <Skeleton className="h-3 w-10" />
              </div>
            </div>
            <div className="grid justify-items-center gap-3 py-6">
              <Skeleton className="h-6 w-3/5" />
              <Skeleton className="h-3 w-2/5" />
              <div className="mt-2 h-7 w-24 rounded-md bg-primary" />
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </div>
          </div>
        </Screen>
      );
  }
}

// The cards in rows of three (or four, when there are four), a short last
// row centred under the others.
function Cards({ items }: { items: Item[] }) {
  const four = items.length === 4;
  return (
    <div className="flex flex-wrap justify-center gap-8">
      {items.map((item) => (
        <Pillar
          key={item.heading}
          variant="card"
          icon={icons[item.icon]}
          heading={item.heading}
          description={item.body}
          className={cn(
            "w-full sm:w-[calc(50%-1rem)]",
            four ? "lg:w-[calc(25%-1.5rem)]" : "lg:w-[calc((100%-4rem)/3)]",
          )}
        />
      ))}
    </div>
  );
}

// The capabilities open, without cards: a line over each, as a reference.
function Grid({ items }: { items: Item[] }) {
  return (
    <div className={cn("grid gap-x-10 gap-y-12 sm:grid-cols-2", items.length === 4 ? "lg:grid-cols-4" : "lg:grid-cols-3")}>
      {items.map((item) => (
        <div key={item.heading} className="border-t border-foreground/10 pt-6 dark:border-foreground/20">
          <Pillar icon={icons[item.icon]} heading={item.heading} description={item.body} />
        </div>
      ))}
    </div>
  );
}

// Three steps side by side, each a tile with its number, an arrow between.
function Columns({ steps }: { steps: Step[] }) {
  return (
    <ol className="page-glow mx-auto grid w-full max-w-5xl gap-10 md:grid-cols-3 md:gap-8">
      {steps.map((s, i) => (
        <li key={s.heading} className="relative grid content-start justify-items-center gap-3 text-center">
          <span className="relative flex size-16 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-[0_10px_24px_rgb(255_79_0/0.25)] [&_svg]:size-7">
            {icons[s.icon]}
            <span className="absolute -top-2 -right-2 grid size-6 place-items-center rounded-full bg-background font-mono text-xs font-semibold text-primary ring-1 ring-primary/30 [text-shadow:none]">
              {i + 1}
            </span>
          </span>
          {i < steps.length - 1 && (
            <ChevronRight aria-hidden="true" className="absolute top-5 -right-6 hidden size-6 text-primary/60 md:block" />
          )}
          <h3 className="mt-2 font-heading text-lg font-semibold text-foreground">{s.heading}</h3>
          <p className="max-w-[32ch] text-muted-foreground">{s.body}</p>
        </li>
      ))}
    </ol>
  );
}

// Three ways to choose from, not steps: three glass cards.
function Options({ steps }: { steps: Step[] }) {
  return (
    <div className="grid gap-8 md:grid-cols-3">
      {steps.map((s) => (
        <Pillar key={s.heading} variant="card" icon={icons[s.icon]} heading={s.heading} description={s.body} />
      ))}
    </div>
  );
}

// The steps told as a conversation: what the person says, and what the agent
// does next.
function Chat({ steps }: { steps: Step[] }) {
  const [ask, ...rest] = steps;
  return (
    <AgentWindow>
      <Conversation>
        <ConversationMessage from="person" author={ask.heading}>
          {ask.body}
        </ConversationMessage>
        <ConversationMessage from="agent" author="Your agent" steps={rest.map((r) => ({ label: r.heading, status: "done" as const }))}>
          {rest.map((r) => r.body).join(" ")}
        </ConversationMessage>
      </Conversation>
    </AgentWindow>
  );
}

function Route({ steps }: { steps: Step[] }) {
  return (
    <VerticalRoute
      className="mx-auto w-full max-w-3xl"
      flush
      steps={steps.map((step, i) => ({
        icon: icons[step.icon],
        lit: true,
        children: (
          <div className={cn(glass, "grid gap-2 p-6 pt-[18px] sm:p-8 sm:pt-[18px]")}>
            <p className="text-sm leading-5 font-medium text-primary">Step {i + 1}</p>
            <h3 className="text-xl font-semibold text-foreground">{step.heading}</h3>
            <p className="max-w-prose text-muted-foreground">{step.body}</p>
          </div>
        ),
      }))}
    />
  );
}

// The questions all in view, side by side: each with a line at its left.
function QuestionColumns({ items }: { items: Landing["questions"]["items"] }) {
  return (
    <ul className={cn("grid gap-8 sm:grid-cols-2", items.length === 3 ? "lg:grid-cols-3" : "lg:grid-cols-4")}>
      {items.map((item) => (
        <li key={item.q} className="border-l-2 pl-4">
          <p className="font-semibold">{item.q}</p>
          <p className="mt-1 max-w-prose text-muted-foreground">{item.a}</p>
        </li>
      ))}
    </ul>
  );
}

export function LandingPage({ page }: { page: Landing }) {
  const design = designs[page.slug] ?? plain;
  const split = design.hero !== "center";
  const actions = (
    <>
      <Button size="lg" asChild>
        <a href={to(page.primary)}>{page.primary.label}</a>
      </Button>
      {page.secondary && (
        <Button size="lg" variant="outline" asChild>
          <a href={to(page.secondary)}>{page.secondary.label}</a>
        </Button>
      )}
    </>
  );
  const steps = page.example.steps;
  return (
    <>
      {design.background === "circle" ? (
        // Infrastructure that turns on its own: the circle of containers,
        // fixed behind the page, turning with the scroll.
        <ContainerCircle />
      ) : (
        // The binary ship behind the top, as on the solutions-backup pages:
        // centred, its top 50px under the header, the rain softly around it,
        // leaning toward the pointer.
        <BinaryMark height={991} offset={122} x={0} near={4} tilt />
      )}
      <PageLayoutContent width="xlarge" padding="normal" className={cn(pageSections, "hero-glow")}>
        {split ? (
          <Hero
            variant="page"
            label={page.label}
            heading={page.heading}
            description={page.description}
            actions={actions}
            image={<Picture kind={design.hero as NonNullable<Design["picture"]>} />}
          />
        ) : (
          <Hero variant="page" align="center" label={page.label} heading={page.heading} description={page.description} actions={actions} />
        )}

        <Stack gap="spacious" className="page-glow">
          <SectionIntro align="center" variant="xlarge" heading={page.example.heading} description={page.example.description} />
          {design.example === "loop" ? (
            // Steps that come round again: the developers' loop (loop-steps.tsx).
            <LoopSteps
              className="mx-auto w-full max-w-5xl"
              steps={steps.map((step) => ({ icon: icons[step.icon], heading: step.heading, body: step.body }))}
            />
          ) : design.example === "columns" ? (
            <Columns steps={steps} />
          ) : design.example === "options" ? (
            <Options steps={steps} />
          ) : design.example === "chat" ? (
            <Chat steps={steps} />
          ) : (
            <Route steps={steps} />
          )}
        </Stack>

        {design.capabilities === "checklist" ? (
          <Checklist
            // The picture as far from the card's top and side as from its
            // foot (16px), filling the height the list gives it.
            className={cn(glass, "[&_[data-slot=checklist-picture]]:!p-4")}
            heading={page.capabilities.heading}
            items={page.capabilities.items.map((item) => (
              <span key={item.heading}>
                <strong className="font-semibold text-foreground">{item.heading}.</strong> {item.body}
              </span>
            ))}
            picture={design.picture ? <Picture kind={design.picture} className="h-full" /> : undefined}
          />
        ) : (
          <Stack gap="spacious">
            <SectionIntro align="center" variant="xlarge" heading={page.capabilities.heading} description={page.capabilities.description} />
            {design.capabilities === "grid" ? <Grid items={page.capabilities.items} /> : <Cards items={page.capabilities.items} />}
          </Stack>
        )}

        <Stack gap="spacious">
          <SectionIntro align="center" variant="xlarge" heading={page.questions.heading} />
          {design.questions === "columns" ? <QuestionColumns items={page.questions.items} /> : <Faq items={page.questions.items} />}
        </Stack>

        {design.close === "shipyard" ? (
          // The large CTA block, with the shipyard: for the pages about
          // running things for you.
          <ShipyardCta
            heading={page.close.heading}
            description={page.close.description}
            actions={
              <Button size="lg" asChild>
                <a href={to(page.close.action)}>{page.close.action.label}</a>
              </Button>
            }
            note={
              <span className="flex flex-wrap justify-center gap-x-5 gap-y-1">
                {page.related.map((l) => (
                  <a key={l.href} href={l.href} className="underline-offset-4 hover:text-foreground hover:underline">
                    {l.label}
                  </a>
                ))}
              </span>
            }
          />
        ) : (
          <SimpleCta
            heading={page.close.heading}
            description={page.close.description}
            action={
              <Button size="lg" asChild>
                <a href={to(page.close.action)}>{page.close.action.label}</a>
              </Button>
            }
            links={page.related}
          />
        )}
      </PageLayoutContent>
    </>
  );
}
