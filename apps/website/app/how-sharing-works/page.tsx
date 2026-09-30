import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Blankslate } from "@shpyrd/ui/components/blankslate";
import { Button } from "@shpyrd/ui/components/button";
import { Stack } from "@shpyrd/ui/components/stack";
import { boundariesForStep } from "@shpyrd/content/site/boundaries";
import { intro, runs, steps } from "@shpyrd/content/site/sharing";
import { Boundaries } from "@/components/boundaries";

export const metadata = { title: "How sharing works", description: intro.lead };

export default function HowSharingWorks() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16 py-8">
      <PageHeading variant="large" title={intro.title} description={intro.lead} />

      {/* Four steps in the order they happen, so they are numbered. Nothing
          else on the site is. */}
      <ol className="grid gap-16">
        {steps.map((step, index) => {
          const limits = boundariesForStep(step.step);
          return (
            <li key={step.id} className="grid gap-6 border-t pt-8 lg:grid-cols-2 lg:gap-12">
              <Stack gap="normal">
                <p className="text-sm text-muted-foreground">Step {index + 1}</p>
                <h2 className="max-w-[18ch] text-2xl font-semibold">{step.title}</h2>
                <p className="max-w-prose text-muted-foreground">{step.body}</p>
                {limits.length > 0 && <Boundaries items={limits} />}
              </Stack>
              <div>
                {step.screenshot.exists ? (
                  /* eslint-disable-next-line @next/next/no-img-element */
                  <img
                    src={step.screenshot.src}
                    alt={step.screenshot.alt}
                    loading="lazy"
                    className="w-full rounded-lg border"
                  />
                ) : (
                  <Blankslate
                    border
                    title="Screenshot pending"
                    description={step.screenshot.note ?? step.screenshot.alt}
                  />
                )}
              </div>
            </li>
          );
        })}
      </ol>

      <Stack gap="normal" className="border-t pt-8">
        <h2 className="text-2xl font-semibold">{runs.title}</h2>
        <p className="max-w-prose text-muted-foreground">{runs.body}</p>
        <Boundaries items={boundariesForStep(runs.step)} />
        <Button variant="link" asChild className="justify-self-start px-0">
          <a href={runs.link.href}>{runs.link.label}</a>
        </Button>
      </Stack>
    </PageLayoutContent>
  );
}
