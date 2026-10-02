// The pricing page: shpyrd cloud's plans and its usage prices.
//
// The figures are the platform's plans (RFC-0075, `plans` in the control
// plane), copied by hand on 2026-10-02:
//
//   NAME     FREE  CPU/CORE-H  MEM/GIB-H  STORAGE/GIB-MO  EGRESS/GIB  MIN/MO  SLEEP        DB SLEEP  SELF-SERVE  LIMITS
//   free     yes   0           0          0               0           0       10m, page    10m       yes         4 projects, 256Mi memory, 2 compute units (2026-10-02)
//   starter  no    0.02        0.005      0.10            0.05        5.00    15m, page    -         no          -
//
// What is charged (confirmed by the maintainer, 2026-10-02): compute by what a
// project actually used (core-seconds), memory by what it reserved - the
// memory of its size - while awake, storage as provisioned (asleep or not),
// HTTP egress at the front door. A sleeping process uses no compute and holds
// no memory. The minimum is a floor on the month's
// total, not a fee on top of it. When a plan changes, change it here.

export const hoursPerMonth = 730

export const prices = {
  cpuCoreHour: 0.02,
  memoryGibHour: 0.005,
  storageGibMonth: 0.1,
  egressGib: 0.05,
}

export const pricing = {
  title: 'Pricing',
  hero: {
    heading: 'Start free. Pay for what runs.',
    description:
      'Put your first app online for nothing. When you need more, you pay for what your apps actually use - and an app nobody is using sleeps, and costs nothing to run.',
  },
  plans: [
    {
      id: 'free',
      name: 'Free',
      price: '0',
      per: 'forever',
      summary: 'A few apps, online and shared, at no cost.',
      features: [
        'Up to 4 projects',
        'Up to 256 MiB of memory and 2 compute units, across them',
        'Its own address, with your sign-in in front',
        'Sleeps after 10 minutes without visits, and wakes on the next one',
        'A database that sleeps after 10 minutes, too',
      ],
      usage: [
        { id: 'compute', value: 'Included' },
        { id: 'memory', value: 'Included' },
        { id: 'storage', value: 'Included' },
        { id: 'egress', value: 'Included' },
      ],
      // The "Add to" button of the home page: it downloads the installer.
      action: { label: 'Add to', kind: 'install' as const },
    },
    {
      id: 'starter',
      name: 'Starter',
      price: '5',
      per: 'a month minimum, then usage',
      summary: 'Your team’s apps, as many as you need, billed by what they use.',
      features: [
        'As many projects and instances as you need',
        'Every size, up to dedicated compute',
        'Apps sleep after 15 minutes idle by default - change it per app, or keep them awake',
        'Databases stay awake',
        'The first $5 of usage is covered by the minimum',
      ],
      usage: [
        { id: 'compute', value: '$0.02 / compute unit-hour' },
        { id: 'memory', value: '$0.005 / GiB-hour' },
        { id: 'storage', value: '$0.10 / GiB-month' },
        { id: 'egress', value: '$0.05 / GiB' },
      ],
      action: { label: 'Talk to us', kind: 'contact' as const },
      note: 'Starter is opened by hand while shpyrd is in beta.',
    },
  ],
  // What each line of a plan's usage is, and how it is measured; the rates are
  // the plans' own.
  usage: [
    { id: 'compute', name: 'Compute', note: 'by actual use' },
    { id: 'memory', name: 'Memory', note: 'reserved, while awake' },
    { id: 'storage', name: 'Storage', note: 'awake or asleep' },
    { id: 'egress', name: 'Traffic out', note: 'sent to visitors' },
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
        body: 'On Starter the month costs at least $5. Usage up to $5 is covered by it; above that, you pay the usage.',
      },
    ],
  },
  example: {
    heading: 'An example',
    description:
      'A tracker your team uses during working hours, with a database. The figures are an estimate: yours come from what your apps actually do.',
    lines: [
      { what: 'The app, awake about 200 hours a month: a twentieth of a compute unit in use, 256 MiB reserved', cpuCores: 0.05, memoryGib: 0.25, hours: 200 },
      { what: 'Its database, awake all month: a hundredth of a compute unit in use, 256 MiB reserved', cpuCores: 0.01, memoryGib: 0.25, hours: hoursPerMonth },
      { what: '5 GiB of data', storageGib: 5 },
      { what: '2 GiB sent to visitors', egressGib: 2 },
    ],
    minimum: 5,
    // The rates at a scale people picture.
    sense: 'For a sense of it: one compute unit busy all month is $14.60, 1 GiB of memory reserved all month $3.65, a 5 GiB database $0.50 a month, and 10 GiB sent to visitors $0.50.',
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
