import { Briefcase, Building2, Check, Gauge, MessageCircle, Moon, Receipt, Rocket, Sprout, Zap } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card } from "@shpyrd/ui/components/card";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { PricingOption, PricingOptions } from "@shpyrd/ui/components/pricing-options";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { Stack } from "@shpyrd/ui/components/stack";
import { contact } from "@shpyrd/content/site/offer";
import { Faq } from "@/components/faq";
import { cn } from "@shpyrd/ui/lib/cn";
import { lineCost, money, pricingFor, type Region } from "@shpyrd/content/site/pricing";
import { glass } from "@shpyrd/ui/lib/glass";
import { AddToAgent } from "@/components/add-to-agent";
import { BinaryOcean } from "@/components/binary-ocean";

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
// The icon of each plan, in a tile like Enterprise's.
const planIcons: Record<string, React.ReactElement> = {
  free: <Sprout />,
  starter: <Rocket />,
  pro: <Zap />,
  business: <Briefcase />,
};
function PlanTile({ children }: { children: React.ReactNode }) {
  return (
    <span
      aria-hidden="true"
      className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground [&_svg]:size-5"
    >
      {children}
    </span>
  );
}

// A plan under the pointer: its edge turns orange, and a soft orange glow
// shows around it, over the glass's own light and shadow.
const lift =
  "transition-[box-shadow] duration-300 hover:ring-primary dark:hover:ring-primary hover:shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_10px_30px_rgb(20_20_30/0.06),0_0_28px_-6px_rgb(255_79_0/0.45)] dark:hover:shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_10px_30px_rgb(0_0_0/0.6),0_0_28px_-6px_rgb(255_79_0/0.5)]";

export function Pricing({ region: id = "international" }: { region?: Region["id"] }) {
  const { region, hero, plans, enterprise, usage, footnote, counts, example, faq } = pricingFor(id);
  const cost = (line: (typeof example.lines)[number]) => lineCost(region.rates.starter, line);
  const total = example.lines.reduce((sum, line) => sum + cost(line), 0);
  const amount = (n: number) => money(region, n, 2);

  return (
    <PageLayoutContent width="xlarge" padding="normal" className="grid grid-cols-1 content-start gap-16 py-8">
      <Hero variant="page" heading={hero.heading} description={hero.description} align="center" />

      {/* The plans float as glass over the binary ocean of the home page, which
          runs from edge to edge behind them. */}
      <Stack gap="normal" className="relative isolate">
        <BinaryOcean
          horizon={0.3}
          className="pointer-events-none absolute -top-16 left-1/2 -z-10 h-[calc(100%+8rem)] w-screen -translate-x-1/2 [mask-image:linear-gradient(to_bottom,transparent,black_12%,black_80%,transparent)]"
        />
        <PricingOptions variant="cards" align="center">
          {plans.map((plan) => (
            <PricingOption
              key={plan.id}
              as="h2"
              heading={
                <span className="flex items-center gap-3">
                  <PlanTile>{planIcons[plan.id]}</PlanTile>
                  {plan.name}
                </span>
              }
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
              className={cn(glass, lift, "[&_[data-slot=pricing-option-heading]]:text-2xl group-data-[variant=cards]/pricing:py-6",
                // The prices on one line across the plans, however long the
                // words over them: the words take the room left, the price
                // sits at the foot of it, over what it is for.
                "[&>div:first-child]:content-stretch [&>div:first-child]:grid-rows-[auto_1fr_auto] [&_[data-slot=pricing-option-price]]:flex-col [&_[data-slot=pricing-option-price]]:items-center",
                "group-data-[variant=cards]/pricing:bg-background/60 dark:group-data-[variant=cards]/pricing:bg-card/60")}
            />
          ))}
        </PricingOptions>
        {/* Enterprise, under the plans: who it is for, what it brings, and a
            conversation. Padded like the plans' cards, so the two rows line up. */}
        <Card className={cn(glass, lift, "@container/enterprise bg-background/60 px-6 py-6 dark:bg-card/60")}>
          <div className="grid gap-6 @3xl/enterprise:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto] @3xl/enterprise:items-center">
            <div className="flex gap-4">
              <PlanTile>
                <Building2 />
              </PlanTile>
              <div className="grid gap-1">
                <h2 className="font-heading text-2xl font-semibold tracking-tight">{enterprise.name}</h2>
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

      {/* The spacing of the home page: 168px between sections (the page's
          64px and 104px more), 56px from a title to what it introduces. */}
      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={counts.heading} />
        {/* In the glass of the home page's blocks, in their type: the icon
            beside the name, the words under it. */}
        <div className="mt-8 grid gap-4 sm:grid-cols-3">
          {counts.items.map((item) => (
            <article key={item.id} className={cn(glass, "grid content-start gap-2 p-6")}>
              <h3 className="flex items-center gap-2 text-base font-semibold text-foreground [&_svg]:size-[18px] [&_svg]:shrink-0 [&_svg]:text-primary">
                {countIcons[item.id]}
                {item.title}
              </h3>
              <p className="text-sm text-muted-foreground">{item.body}</p>
            </article>
          ))}
        </div>
      </Stack>

      <Stack gap="spacious" className="mt-26">
        <SectionIntro variant="xlarge" align="center" heading={example.heading} description={example.description} />
        {/* The example as an invoice: what each part used, line by line,
            then a strong rule, and under it the sums - the usage, and what is
            paid: the usage, or the plan's minimum when the usage is under it. */}
        <div className={cn(glass, "mx-auto mt-8 w-full max-w-4xl overflow-x-auto p-6 sm:p-8")}>
          <div className="flex items-start justify-between gap-4 border-b pb-5">
            <div className="flex items-center gap-3">
              <PlanTile>
                <Receipt />
              </PlanTile>
              <div className="grid">
                <span className="font-heading text-lg font-semibold">Invoice</span>
                <span className="text-sm text-muted-foreground">One month</span>
              </div>
            </div>
            <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">Estimate</span>
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs tracking-wide text-muted-foreground uppercase">
                <th scope="col" className="py-3 text-left font-medium">What was used</th>
                <th scope="col" className="py-3 text-right font-medium">Amount</th>
              </tr>
            </thead>
            <tbody>
              {example.lines.map((line, i) => (
                <tr key={line.what} className="border-t border-foreground/8">
                  <td className="py-3 pr-6">
                    <span className="flex items-baseline gap-3">
                      <span className="font-mono text-xs text-muted-foreground tabular-nums">{String(i + 1).padStart(2, "0")}</span>
                      <span>{line.what}</span>
                    </span>
                  </td>
                  <td className="py-3 text-right tabular-nums">{amount(cost(line))}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr className="border-t-2 border-foreground">
                <td className="py-4 text-muted-foreground">Usage for the month, lines 01 to 04</td>
                <td className="py-4 text-right tabular-nums">{amount(total)}</td>
              </tr>
              <tr className="border-t border-foreground/8">
                <td className="pt-4">
                  <span className="font-heading text-xl font-semibold">You pay</span>
                  <span className="ml-2 text-muted-foreground">
                    {total < example.minimum ? "the Starter minimum" : "the usage"}
                  </span>
                </td>
                <td className="pt-4 text-right font-heading text-2xl font-semibold tabular-nums">
                  {amount(Math.max(total, example.minimum))}
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
        <p className="mx-auto max-w-4xl text-center text-sm text-balance text-muted-foreground">{example.sense}</p>
      </Stack>

      {/* The last section is 168px from the footer's line too. */}
      <Stack gap="spacious" className="mt-26 mb-36">
        <SectionIntro
          variant="xlarge"
          align="center"
          heading="Frequently asked questions"
          description="What people ask most before they start."
        />
        <Faq items={faq} className="mt-8" />
        <p className="text-center text-sm text-muted-foreground">
          Another question?{" "}
          <a href={contact.href} className="font-medium text-foreground underline underline-offset-4 hover:text-primary">
            Talk to us
          </a>
        </p>
      </Stack>
    </PageLayoutContent>
  );
}
