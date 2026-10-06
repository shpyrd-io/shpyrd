import { Building2, Check, Gauge, MessageCircle, Moon, Receipt } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Pillar } from "@shpyrd/ui/components/pillar";
import { PricingOption, PricingOptions } from "@shpyrd/ui/components/pricing-options";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableFooter, TableRow } from "@shpyrd/ui/components/table";
import { contact } from "@shpyrd/content/site/offer";
import { lineCost, money, pricingFor, type Region } from "@shpyrd/content/site/pricing";
import { AddToAgent } from "@/components/add-to-agent";

// The pricing page: the plans of shpyrd cloud with the usage prices of the
// paid ones, each with the "Add to" button of the home page, Enterprise as a
// conversation in a row of its own under them,
// what is counted, a worked example and the usual questions. Every figure comes from content/site/pricing.ts.

const countIcons: Record<string, React.ReactElement> = {
  use: <Gauge />,
  sleep: <Moon />,
  minimum: <Receipt />,
};

// `region` picks the prices: Brazil has its own (/pricing/br, where
// vercel.json sends visitors from Brazil), everyone else the international ones.
export function Pricing({ region: id = "international" }: { region?: Region["id"] }) {
  const { region, hero, plans, enterprise, usage, footnote, counts, example, faq } = pricingFor(id);
  const cost = (line: (typeof example.lines)[number]) => lineCost(region.rates.starter, line);
  const total = example.lines.reduce((sum, line) => sum + cost(line), 0);
  const amount = (n: number) => money(region, n, 2);

  return (
    <PageLayoutContent width="large" padding="normal" className="grid grid-cols-1 content-start gap-16 py-8">
      <Hero heading={hero.heading} description={hero.description} align="center" />

      <Stack gap="normal">
        <PricingOptions variant="cards">
          {plans.map((plan) => (
            <PricingOption
              key={plan.id}
              as="h2"
              heading={plan.name}
              description={plan.summary}
              price={plan.price}
              currencySymbol={region.symbol}
              trailingText={plan.per}
              features={plan.features.map((feature) => ({ children: feature }))}
              usage={plan.usage.map((rate) => ({
                name: usage.find((line) => line.id === rate.id)?.name ?? rate.id,
                note: usage.find((line) => line.id === rate.id)?.note,
                value: rate.value,
              }))}
              // Centred in the card, whatever the plan's action is.
              actions={
                <div className="flex flex-1 justify-center">
                  {plan.action.kind === "install" ? (
                    <AddToAgent />
                  ) : (
                    <Button asChild variant="outline" icon={<MessageCircle />}>
                      <a href={contact.href}>{plan.action.label}</a>
                    </Button>
                  )}
                </div>
              }
              message={plan.note}
              footnote={plan.footnote}
            />
          ))}
        </PricingOptions>
        {/* Enterprise, under the plans: who it is for, what it brings, and a
            conversation. Padded like the plans' cards, so the two rows line up. */}
        <Card className="@container/enterprise px-6 py-6">
          <div className="grid gap-6 @3xl/enterprise:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto] @3xl/enterprise:items-center">
            <div className="flex gap-4">
              <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground">
                <Building2 aria-hidden="true" className="size-5" />
              </div>
              <div className="grid gap-1">
                <h2 className="font-heading text-xl">{enterprise.name}</h2>
                <p className="text-sm text-muted-foreground">{enterprise.summary}</p>
              </div>
            </div>
            <ul className="grid gap-2.5 text-sm @3xl/enterprise:border-l @3xl/enterprise:pl-6">
              {enterprise.features.map((feature) => (
                <li key={feature} className="flex gap-2.5">
                  <Check aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
                  <span>{feature}</span>
                </li>
              ))}
            </ul>
            <Button asChild variant="outline" icon={<MessageCircle />} className="justify-self-start @3xl/enterprise:justify-self-end">
              <a href={contact.href}>{enterprise.action.label}</a>
            </Button>
          </div>
        </Card>
        <p className="text-center text-xs text-muted-foreground">
          {footnote.text}{" "}
          <a href={footnote.other.href} className="underline underline-offset-4 hover:text-foreground">
            {footnote.other.label}
          </a>
        </p>
      </Stack>

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
                  <TableCell className="text-right tabular-nums">{amount(cost(line))}</TableCell>
                </TableRow>
              ))}
            </TableBody>
            <TableFooter>
              <TableRow>
                <TableCell>Usage for the month</TableCell>
                <TableCell className="text-right tabular-nums">{amount(total)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell className="font-medium">
                  You pay {total < example.minimum ? "the Starter minimum" : "the usage"}
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">
                  {amount(Math.max(total, example.minimum))}
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
