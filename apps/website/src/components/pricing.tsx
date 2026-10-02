import { Gauge, Moon, Receipt } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { PricingOption, PricingOptions } from "@shpyrd/ui/components/pricing-options";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableFooter, TableRow } from "@shpyrd/ui/components/table";
import { contact } from "@shpyrd/content/site/offer";
import { lineCost, pricing } from "@shpyrd/content/site/pricing";
import { AddToAgent } from "@/components/add-to-agent";

// The pricing page: the two plans of shpyrd cloud, the usage prices of the
// paid one, what is counted, a worked example and the usual questions. Every
// figure comes from content/site/pricing.ts.

const countIcons: Record<string, React.ReactElement> = {
  use: <Gauge />,
  sleep: <Moon />,
  minimum: <Receipt />,
};

const dollars = (n: number) => `$${n.toFixed(2)}`;

export function Pricing() {
  const { hero, plans, usage, footnote, counts, example, faq } = pricing;
  const total = example.lines.reduce((sum, line) => sum + lineCost(line), 0);

  return (
    <PageLayoutContent width="large" padding="normal" className="grid grid-cols-1 content-start gap-16 py-8">
      <Hero heading={hero.heading} description={hero.description} align="center" />

      <PricingOptions variant="cards">
        {plans.map((plan) => (
          <PricingOption
            key={plan.id}
            as="h2"
            heading={plan.name}
            description={plan.summary}
            price={plan.price}
            trailingText={plan.per}
            features={plan.features.map((feature) => ({ children: feature }))}
            usage={plan.usage.map((rate) => ({
              name: usage.find((line) => line.id === rate.id)?.name ?? rate.id,
              note: usage.find((line) => line.id === rate.id)?.note,
              value: rate.value,
            }))}
            actions={
              plan.action.kind === "install" ? (
                <AddToAgent />
              ) : (
                <Button asChild variant="outline">
                  <a href={contact.href}>{plan.action.label}</a>
                </Button>
              )
            }
            message={plan.note}
            footnote={plan.id === "starter" ? footnote : undefined}
          />
        ))}
      </PricingOptions>

      <Stack gap="spacious">
        <SectionIntro heading={counts.heading} />
        <div className="grid gap-8 sm:grid-cols-3">
          {counts.items.map((item) => (
            <Pillar key={item.id} icon={countIcons[item.id]} heading={item.title} description={item.body} />
          ))}
        </div>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro heading={example.heading} description={example.description} />
        <div className="overflow-x-auto">
          <Table>
            <TableBody>
              {example.lines.map((line) => (
                <TableRow key={line.what}>
                  <TableCell className="whitespace-normal">{line.what}</TableCell>
                  <TableCell className="text-right tabular-nums">{dollars(lineCost(line))}</TableCell>
                </TableRow>
              ))}
            </TableBody>
            <TableFooter>
              <TableRow>
                <TableCell>Usage for the month</TableCell>
                <TableCell className="text-right tabular-nums">{dollars(total)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell className="font-medium">
                  You pay {total < example.minimum ? "the Starter minimum" : "the usage"}
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">
                  {dollars(Math.max(total, example.minimum))}
                </TableCell>
              </TableRow>
            </TableFooter>
          </Table>
        </div>
        <p className="max-w-prose text-sm text-muted-foreground">{example.sense}</p>
      </Stack>

      <Stack gap="spacious">
        <SectionIntro heading="Questions" />
        <dl className="grid gap-8 md:grid-cols-2">
          {faq.map((item) => (
            <div key={item.q} className="grid gap-2">
              <dt className="font-medium">{item.q}</dt>
              <dd className="text-muted-foreground">{item.a}</dd>
            </div>
          ))}
        </dl>
      </Stack>
    </PageLayoutContent>
  );
}
