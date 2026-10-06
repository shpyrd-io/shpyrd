// The pricing page: shpyrd cloud's plans and its usage prices, by region.
// Prices are regional, not converted: Brazil has its own, in reais.
//
// The figures are the plans the maintainer gave:
//
//   USD (2026-10-05)
//   NAME      MIN/MO  CPU/CORE-H  MEM/GIB-H  STORAGE/GIB-MO  EGRESS/GIB  CEILINGS                                INVOICED EARLY PAST
//   free      0       included    included   included        included    4 projects, 2 cores, 256 MiB            -
//   starter   5       0.09        0.02       0.17            0.05        16 cores, 32 GiB, 2048 GiB of storage   $25 owed at first
//   pro       49      0.075       0.01       0.16            0.04        32 cores, 64 GiB, 4096 GiB of storage   $300 owed at first
//   business  499     0.055       0.005      0.15            0.035       none                                    $1,500 owed at first
//
//   Business's minimum and rates are the maintainer's (2026-10-05); its
//   early invoicing is the billing app's (shpyrd-billing, mock/plans.json).
//   The plans' features are the maintainer's (2026-10-05).
//
//   BRL, Brazil (2026-10-05)
//   NAME      MIN/MO  CPU/CORE-H  MEM/GIB-H  STORAGE/GIB-MO  EGRESS/GIB
//   free      0       included    included   included        included
//   starter   25      0,486       0,108      0,918           0,27
//   pro       199     0,405       0,054      0,864           0,216
//   business  2499    0,297       0,027      0,81            0,189
//
// Enterprise has no price on the page: it is a conversation, in a row of its
// own under the plans.
//
// What is charged (confirmed by the maintainer, 2026-10-02): compute by what a
// project actually used (core-seconds), memory by what it reserved - the
// memory of its size - while awake, storage as provisioned (asleep or not),
// HTTP egress at the front door. A sleeping process uses no compute and holds
// no memory. The minimum is a floor on the month's total, not a fee on top of
// it. When a plan changes, change it here.

export const hoursPerMonth = 730

type Rates = { cpuCoreHour: number; memoryGibHour: number; storageGibMonth: number; egressGib: number }

type PaidPlan = 'starter' | 'pro' | 'business'

// A region: its currency, and the minimums and rates of the paid plans in it.
export type Region = {
  id: 'international' | 'br'
  currency: 'USD' | 'BRL'
  symbol: '$' | 'R$'
  minimum: Record<PaidPlan, number>
  rates: Record<PaidPlan, Rates>
  // When a paid plan is invoiced before the month ends, by what is owed at
  // first; not set where it has not been given.
  invoicedEarly?: Record<PaidPlan, number>
}

export const regions: Record<Region['id'], Region> = {
  international: {
    id: 'international',
    currency: 'USD',
    symbol: '$',
    minimum: { starter: 5, pro: 49, business: 499 },
    rates: {
      starter: { cpuCoreHour: 0.09, memoryGibHour: 0.02, storageGibMonth: 0.17, egressGib: 0.05 },
      pro: { cpuCoreHour: 0.075, memoryGibHour: 0.01, storageGibMonth: 0.16, egressGib: 0.04 },
      business: { cpuCoreHour: 0.055, memoryGibHour: 0.005, storageGibMonth: 0.15, egressGib: 0.035 },
    },
    invoicedEarly: { starter: 25, pro: 300, business: 1500 },
  },
  br: {
    id: 'br',
    currency: 'BRL',
    symbol: 'R$',
    minimum: { starter: 25, pro: 199, business: 2499 },
    rates: {
      starter: { cpuCoreHour: 0.486, memoryGibHour: 0.108, storageGibMonth: 0.918, egressGib: 0.27 },
      pro: { cpuCoreHour: 0.405, memoryGibHour: 0.054, storageGibMonth: 0.864, egressGib: 0.216 },
      business: { cpuCoreHour: 0.297, memoryGibHour: 0.027, storageGibMonth: 0.81, egressGib: 0.189 },
    },
  },
}

// A number as the region writes it: "1,500.50" or "1.500,50".
function number(region: Region, n: number, digits: number): string {
  return n.toLocaleString(region.currency === 'BRL' ? 'pt-BR' : 'en-US', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
}

// An amount as the region writes it: "$0.09", "$1,500", "R$ 0,108". `digits`
// fixes the decimals (totals); without it the amount keeps its own.
export function money(region: Region, n: number, digits?: number): string {
  const text = number(region, n, digits ?? (String(n).split('.')[1]?.length ?? 0))
  // A non-breaking space: "R$" never ends a line apart from its amount.
  return region.currency === 'BRL' ? `R$\u00a0${text}` : `$${text}`
}

// The price on a plan's card, without the symbol: "5", "264,60", "1.344,60".
function priceOf(region: Region, n: number): string {
  return number(region, n, Number.isInteger(n) ? 0 : 2)
}

type Plan = {
  id: string
  name: string
  price: string
  per: string
  summary: string
  features: string[]
  usage: { id: string; value: string }[]
  action: { label: string; kind: 'install' | 'contact' }
  note?: string
  footnote?: string
}

const included = [
  { id: 'cpu', value: 'Included' },
  { id: 'memory', value: 'Included' },
  { id: 'storage', value: 'Included' },
  { id: 'egress', value: 'Included' },
]

function plansFor(region: Region): Plan[] {
  const usageOf = (r: Rates) => [
    { id: 'cpu', value: money(region, r.cpuCoreHour) },
    { id: 'memory', value: money(region, r.memoryGibHour) },
    { id: 'storage', value: money(region, r.storageGibMonth) },
    { id: 'egress', value: money(region, r.egressGib) },
  ]
  const early = (plan: PaidPlan) =>
    region.invoicedEarly ? ` Invoiced early past ${money(region, region.invoicedEarly[plan])} owed at first.` : ''
  return [
    {
      id: 'free',
      name: 'Free',
      price: '0',
      per: 'a month',
      summary: 'To try the platform: apps that sleep when nobody uses them.',
      features: ['4 projects', '2 CPU and 256 MiB in all', 'Sleeps after 10 minutes idle', 'Community support'],
      usage: included,
      // Every plan starts the same way, with the "Add to" button of the home
      // page: it downloads the installer. Only Enterprise is a conversation.
      action: { label: 'Add to', kind: 'install' },
      footnote: 'Ceilings: Projects 4 · CPU 2 cores · Memory 256 MiB.',
    },
    {
      id: 'starter',
      name: 'Starter',
      price: priceOf(region, region.minimum.starter),
      per: 'a month minimum, then usage',
      summary: `Pay for what runs, from ${money(region, region.minimum.starter)} a month.`,
      features: ['Unlimited projects', 'Sleeps after 15 minutes idle', 'Custom domains', 'Email support'],
      usage: usageOf(region.rates.starter),
      action: { label: 'Add to', kind: 'install' },
      footnote: `Ceilings: CPU 16 cores · Memory 32 GiB · Storage 2048 GiB.${early('starter')}`,
    },
    {
      id: 'pro',
      name: 'Pro',
      price: priceOf(region, region.minimum.pro),
      per: 'a month minimum, then usage',
      summary: 'For products in production.',
      features: ['Custom sleep', 'Daily backups', 'Priority support', 'SSO and audit logs'],
      usage: usageOf(region.rates.pro),
      action: { label: 'Add to', kind: 'install' },
      footnote: `Ceilings: CPU 32 cores · Memory 64 GiB · Storage 4096 GiB.${early('pro')}`,
    },
    {
      id: 'business',
      name: 'Business',
      price: priceOf(region, region.minimum.business),
      per: 'a month minimum, then usage',
      summary: 'For teams that run their business on it.',
      features: ['Hourly backups', 'Private network applications', 'Support with an SLA', 'VPC peering'],
      usage: usageOf(region.rates.business),
      action: { label: 'Add to', kind: 'install' },
      footnote: `No ceilings.${early('business')}`,
    },
  ]
}

// The page as a region sees it.
export function pricingFor(id: Region['id']) {
  const region = regions[id]
  const starter = region.rates.starter
  const m = (n: number) => money(region, n, 2)
  return {
    region,
    title: 'Pricing',
    hero: {
      heading: 'Start free. Pay for what runs.',
      description:
        'Put your first app online for nothing. When you need more, you pay for what your apps actually use - and an app nobody is using sleeps, and costs nothing to run.',
    },
    plans: plansFor(region),
    // Enterprise is shpyrd in a cluster of the customer's own with the
    // enterprise features switched on by a license: the four of ee/ (sso,
    // autosleep, costs, mcp). On shpyrd cloud they are always on, for every
    // plan (content/docs/extensions.md), so they are not what sets it apart there.
    enterprise: {
      name: 'Enterprise',
      summary: 'For custom requirements: shpyrd in your own cloud, on your terms, with everything shpyrd cloud has. A license switches it on and renews itself.',
      features: [
        'Sign-in through Google, Microsoft, GitHub or any OpenID Connect provider',
        'Apps and databases that sleep by themselves',
        'Costs per project, and drains to send them on',
        'The MCP server for AI assistants',
      ],
      action: { label: 'Talk to us', kind: 'contact' as const },
    },
    // What each line of a plan's usage is, and how it is measured; the rates are
    // the plans' own.
    usage: [
      { id: 'cpu', name: 'CPU', note: 'per core-hour, used' },
      { id: 'memory', name: 'Memory', note: 'per GiB-hour, reserved, while awake' },
      { id: 'storage', name: 'Storage', note: 'per GiB-month, volume capacity, awake or asleep' },
      { id: 'egress', name: 'Egress', note: 'per GiB, traffic out of the front door' },
    ],
    // Where the prices are for, and the way to the other region's.
    footnote:
      id === 'br'
        ? { text: 'Prices in BRL, for Brazil.', other: { label: 'Prices in USD', href: '/pricing?currency=usd' } }
        : { text: 'Prices in USD.', other: { label: 'Prices in BRL, for Brazil', href: '/pricing/br' } },
    counts: {
      heading: 'What counts',
      items: [
        {
          id: 'use',
          title: 'Compute by use, memory by size',
          body: 'Compute is measured from what your apps actually use: a bigger size costs nothing more until the app works harder. Memory is what each app reserves - the memory of its size - while it is awake.',
        },
        {
          id: 'sleep',
          title: 'A sleeping app costs nothing to run',
          body: 'An app nobody visits sleeps and wakes on the next visit, in a few seconds. While it sleeps it uses no compute and no memory. Its stored data still counts.',
        },
        {
          id: 'minimum',
          title: 'A minimum, not a fee',
          body: `On Starter the month costs at least ${money(region, region.minimum.starter)}, on Pro ${money(region, region.minimum.pro)}, on Business ${money(region, region.minimum.business)}. Usage up to the minimum is covered by it; above that, you pay the usage.`,
        },
      ],
    },
    example: {
      heading: 'An example',
      description:
        'A tracker your team uses during working hours, with a database. The figures are an estimate: yours come from what your apps actually do.',
      lines: [
        { what: 'The app, awake about 200 hours a month: a twentieth of a CPU core in use, 256 MiB reserved', cpuCores: 0.05, memoryGib: 0.25, hours: 200 },
        { what: 'Its database, awake all month: a hundredth of a CPU core in use, 256 MiB reserved', cpuCores: 0.01, memoryGib: 0.25, hours: hoursPerMonth },
        { what: '5 GiB of data', storageGib: 5 },
        { what: '2 GiB sent to visitors', egressGib: 2 },
      ],
      minimum: region.minimum.starter,
      // The rates at a scale people picture.
      sense: `For a sense of it, on Starter: one CPU core busy all month is ${m(starter.cpuCoreHour * hoursPerMonth)}, 1 GiB of memory reserved all month ${m(starter.memoryGibHour * hoursPerMonth)}, a 5 GiB database ${m(starter.storageGibMonth * 5)} a month, and 10 GiB sent to visitors ${m(starter.egressGib * 10)}.`,
    },
    faq: [
      {
        q: 'What happens when my app outgrows Free?',
        a: 'Free stops at its limits - a fifth project, or more memory or compute than it allows, is refused with a message saying so. Nothing you already run is touched. Move to Starter to go past them.',
      },
      {
        q: 'Does a sleeping app lose anything?',
        a: 'No. Its code, settings and data stay as they are; the next visit wakes it in a few seconds, with a page that says it is waking.',
      },
      {
        q: 'Can I run shpyrd myself instead?',
        a: 'Yes. shpyrd is open source under MPL-2.0 and runs on a Kubernetes cluster of your own, on your laptop, on AWS or on Oracle Cloud. There is nothing to pay us for that.',
      },
      {
        q: 'How am I billed?',
        a: `Monthly, in ${id === 'br' ? 'Brazilian reais' : 'US dollars'}, for the calendar month. Your workspace shows the month so far, project by project.`,
      },
    ],
  }
}

// The page outside Brazil.
export const pricing = pricingFor('international')

// The cost of one line of the example, at the given rates (Starter's, on the
// page).
export function lineCost(rates: Rates, line: {
  cpuCores?: number
  memoryGib?: number
  hours?: number
  storageGib?: number
  egressGib?: number
}): number {
  const hours = line.hours ?? 0
  return (
    (line.cpuCores ?? 0) * hours * rates.cpuCoreHour +
    (line.memoryGib ?? 0) * hours * rates.memoryGibHour +
    (line.storageGib ?? 0) * rates.storageGibMonth +
    (line.egressGib ?? 0) * rates.egressGib
  )
}
