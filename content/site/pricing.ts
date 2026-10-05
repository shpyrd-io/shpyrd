// The pricing page: shpyrd cloud's plans and its usage prices.
//
// The figures are the plans the maintainer gave on 2026-10-05:
//
//   NAME      MIN/MO  CPU/CORE-H  MEM/GIB-H  STORAGE/GIB-MO  EGRESS/GIB  CEILINGS                                INVOICED EARLY PAST
//   free      0       included    included   included        included    4 projects, 2 cores, 256 MiB            -
//   starter   5       0.09        0.02       0.17            0.05        16 cores, 32 GiB, 2048 GiB of storage   $25 owed at first
//   pro       49      0.075       0.01       0.16            0.04        32 cores, 64 GiB, 4096 GiB of storage   $300 owed at first
//
// Enterprise has no price on the page: it is a conversation, in a row of its
// own under the plans.
//
// What is charged (confirmed by the maintainer, 2026-10-02): compute by what a
// project actually used (core-seconds), memory by what it reserved - the
// memory of its size - while awake, storage as provisioned (asleep or not),
// HTTP egress at the front door. A sleeping process uses no compute and holds
// no memory. The minimum is a floor on the month's
// total, not a fee on top of it. When a plan changes, change it here.

export const hoursPerMonth = 730

// The rates of each paid plan, in USD.
export const rates = {
  starter: { cpuCoreHour: 0.09, memoryGibHour: 0.02, storageGibMonth: 0.17, egressGib: 0.05 },
  pro: { cpuCoreHour: 0.075, memoryGibHour: 0.01, storageGibMonth: 0.16, egressGib: 0.04 },
}

// The example is costed on Starter, the plan it is about.
export const prices = rates.starter

const rate = (n: number) => `$${n}`

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

const usageOf = (r: typeof rates.starter) => [
  { id: 'cpu', value: rate(r.cpuCoreHour) },
  { id: 'memory', value: rate(r.memoryGibHour) },
  { id: 'storage', value: rate(r.storageGibMonth) },
  { id: 'egress', value: rate(r.egressGib) },
]

const plans: Plan[] = [
  {
    id: 'free',
    name: 'Free',
    price: '0',
    per: 'a month',
    summary: 'To try the platform: apps that sleep when nobody uses them.',
    features: ['4 projects', '2 CPU and 256 MiB in all', 'Sleeps after 10 minutes idle', 'Community support'],
    usage: included,
    // The "Add to" button of the home page: it downloads the installer.
    action: { label: 'Add to', kind: 'install' },
    footnote: 'Ceilings: Projects 4 · CPU 2 cores · Memory 256 MiB.',
  },
  {
    id: 'starter',
    name: 'Starter',
    price: '5',
    per: 'a month minimum, then usage',
    summary: 'Pay for what runs, from $5 a month.',
    features: ['Unlimited projects', 'Sleeps after 15 minutes idle', 'Custom domains', 'Email support'],
    usage: usageOf(rates.starter),
    action: { label: 'Talk to us', kind: 'contact' },
    note: 'Starter is opened by hand while shpyrd is in beta.',
    footnote: 'Ceilings: CPU 16 cores · Memory 32 GiB · Storage 2048 GiB. Invoiced early past $25 owed at first.',
  },
  {
    id: 'pro',
    name: 'Pro',
    price: '49',
    per: 'a month minimum, then usage',
    summary: 'For products in production, always awake.',
    features: ['Never sleeps', 'Daily backups', 'Priority support'],
    usage: usageOf(rates.pro),
    action: { label: 'Talk to us', kind: 'contact' },
    note: 'Pro is opened by hand while shpyrd is in beta.',
    footnote: 'Ceilings: CPU 32 cores · Memory 64 GiB · Storage 4096 GiB. Invoiced early past $300 owed at first.',
  },
]

export const pricing = {
  title: 'Pricing',
  hero: {
    heading: 'Start free. Pay for what runs.',
    description:
      'Put your first app online for nothing. When you need more, you pay for what your apps actually use - and an app nobody is using sleeps, and costs nothing to run.',
  },
  plans,
  // Enterprise is shpyrd in a cluster of the customer's own with the
  // enterprise features switched on by a license: the four of ee/ (sso,
  // autosleep, costs, mcp). On shpyrd cloud they are always on, for every
  // plan (content/docs/extensions.md), so they are not what sets it apart there.
  enterprise: {
    name: 'Enterprise',
    summary: 'shpyrd in your own cloud, with everything shpyrd cloud has. A license switches it on and renews itself.',
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
  footnote: 'Prices in USD.',
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
        body: 'On Starter the month costs at least $5, on Pro $49. Usage up to the minimum is covered by it; above that, you pay the usage.',
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
    minimum: 5,
    // The rates at a scale people picture.
    sense: 'For a sense of it, on Starter: one CPU core busy all month is $65.70, 1 GiB of memory reserved all month $14.60, a 5 GiB database $0.85 a month, and 10 GiB sent to visitors $0.50.',
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
      a: 'Monthly, in US dollars, for the calendar month. Your workspace shows the month so far, project by project.',
    },
  ],
}

// The cost of one line of the example, from the usage prices.
export function lineCost(line: {
  cpuCores?: number
  memoryGib?: number
  hours?: number
  storageGib?: number
  egressGib?: number
}): number {
  const hours = line.hours ?? 0
  return (
    (line.cpuCores ?? 0) * hours * prices.cpuCoreHour +
    (line.memoryGib ?? 0) * hours * prices.memoryGibHour +
    (line.storageGib ?? 0) * prices.storageGibMonth +
    (line.egressGib ?? 0) * prices.egressGib
  )
}
