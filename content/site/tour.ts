// /small-software-manifesto — the pitch as a deck, one slide a screen, for a company whose
// teams already build apps with AI. After the Doca proposal
// (ai-apps-enterprise.vercel.app), told with what shpyrd does today: no data
// gateway and no masking (shpyrd has none), and sleep in place of replicas at
// the peak (autoscaling is RFC-0047, not shipped).

// The apps the deck follows, slide after slide: a team's own tools.
export const apps = [
  { name: 'expenses', team: 'Finance' },
  { name: 'onboarding', team: 'People' },
  { name: 'pipeline', team: 'Sales' },
  { name: 'nps-board', team: 'Support' },
  { name: 'campaigns', team: 'Marketing' },
  { name: 'contracts', team: 'Legal' },
  { name: 'inventory', team: 'Ops' },
  { name: 'commissions', team: 'Sales' },
  { name: 'shifts', team: 'Ops' },
]

export const opening = {
  label: 'A tour of shpyrd',
  // The heading in three parts: the middle one in the brand's colour.
  heading: ['Welcome to the', 'Small Software', 'era.'],
  lead:
    'Apps from every team in your company, built with the AI they already use, ' +
    'running cheap, fast and safe behind your company’s sign-in.',
  points: ['Company sign-in', 'Roles per app', 'Sleeps when idle', 'No infra to run'],
  start: 'Start the tour',
  before: 'today: 47 apps, no standard',
  after: 'on shpyrd: one place, one sign-in',
}

export const problem = {
  label: 'The problem',
  heading: 'Every team is vibecoding. Nobody is running it.',
  lead:
    'Teams outside engineering build apps with AI in an afternoon. Each one ' +
    'lands somewhere different, in its own way: a link and a password, no ' +
    'roles, open to the internet, and on all night.',
  teams: [
    { name: 'Finance', apps: ['expenses', 'cash-flow', 'reconciliation', 'commissions'] },
    { name: 'Marketing', apps: ['campaigns', 'lead-score', 'landing-ab'] },
    { name: 'People', apps: ['onboarding', 'time-off', 'reviews-360'] },
    { name: 'Sales', apps: ['pipeline', 'proposal-gen', 'quote-lite', 'targets'] },
    { name: 'Ops', apps: ['inventory', 'shifts', 'sla-board'] },
    { name: 'Support', apps: ['nps-board', 'ai-triage'] },
    { name: 'Legal', apps: ['contracts', 'due-diligence'] },
  ],
  hostsLabel: 'Running on',
  hosts: ['replit', 'vercel', 'lovable', 'a spreadsheet', 'an old VM', 'localhost', 'bolt', 'render'],
  // The same company all through the deck: 47 apps.
  total: 47,
  count: 'apps in use · 7 teams · no standard',
  hover: 'Point at an app to see what’s wrong with it',
  // What a pointer on an app reveals: one of these, by the app's place.
  faults: [
    'Anyone with the link gets in',
    'One shared password, in a chat',
    'No idea who opened it',
    'Its database is on all night',
    'The API key is in the code',
    'Runs on someone’s laptop',
  ],
  costs: [
    { title: 'Risk', body: 'A link that leaks opens the company’s data to anyone who has it.' },
    { title: 'Cost', body: 'Dozens of apps and databases on around the clock, used two hours a day.' },
    { title: 'Bottleneck', body: 'IT becomes a queue, or a no. The apps get built in the dark.' },
  ],
}

// The x-ray: what each app is missing, and what shpyrd gives it.
export const xray = {
  label: 'X-ray',
  heading: 'What every vibecoded app is missing',
  lead:
    'It isn’t the builder’s fault: security and infrastructure never come up ' +
    'in the prompt. The platform has to give them, by default, to every app.',
  columns: [
    'Company sign-in',
    'Roles',
    'Network isolation',
    'TLS',
    'Sleeps when idle',
    'Database backups',
    'Rollback',
    'Audit trail',
  ],
  // Which columns each app already has, by index, before shpyrd.
  rows: [
    { app: 'expenses', team: 'Finance', has: [3] },
    { app: 'pipeline', team: 'Sales', has: [0, 3] },
    { app: 'onboarding', team: 'People', has: [3, 4] },
    { app: 'campaigns', team: 'Marketing', has: [] },
    { app: 'inventory', team: 'Ops', has: [0, 1, 3, 7] },
    { app: 'contracts', team: 'Legal', has: [3] },
  ],
  compliance: 'covered',
  ship: 'Ship them all on shpyrd',
  reset: 'Back to today',
}

// The layers every request goes through.
export const platform = {
  label: 'The platform',
  heading: 'A platform where every app starts out safe',
  lead:
    'Every request crosses the same layers, whoever built the app and ' +
    'whichever AI they used.',
  who: 'someone@yourcompany.com',
  across: 'Logs, metrics and the audit trail run through every layer',
  layers: [
    {
      id: 'door',
      name: 'Front door',
      short: 'Your company’s sign-in',
      points: [
        'Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider (Okta, Keycloak, Auth0…)',
        'Your provider’s groups become teams',
        'The app writes no sign-in code: it receives who is there',
        'Nobody who isn’t signed in ever reaches the app',
      ],
    },
    {
      id: 'roles',
      name: 'Roles',
      short: 'People and teams, per app',
      points: [
        'Each app says who may open it: people, teams, or everyone signed in',
        'Open it, or open it read-only; or change it as a developer or admin',
        'Or public, for a site anyone may open',
        'One sentence to your agent shares it',
      ],
    },
    {
      id: 'network',
      name: 'Network policy',
      short: 'Isolated by default',
      points: [
        'Every project gets its own network policy',
        'Reachable only through the front door',
        'Never from another project',
        'Projects talk only through what they choose to share',
      ],
    },
    {
      id: 'runtime',
      name: 'Runtime',
      short: 'Any stack, asleep when idle',
      points: [
        'Node, Python, Go, Ruby, Java, .NET, PHP, or your Dockerfile',
        'Built from the repository as it is: no rewrite',
        'Sleeps when nobody uses it, wakes on the first visit',
        'Every process as a non-root user, with TLS on its own address',
      ],
    },
    {
      id: 'data',
      name: 'Databases',
      short: 'Its own, backed up',
      points: [
        'PostgreSQL for each app that needs one, in a command',
        'Backups and restore, from the dashboard or the CLI',
        'Asleep when nobody is connected: you pay for the disk',
        'Secrets as config vars, never in the code',
      ],
    },
  ],
}

// How it works, from the prompt to the team.
export const flow = {
  label: 'How it works',
  heading: 'From the prompt to the team in minutes',
  lead:
    'Whoever builds it never has to know what Kubernetes, IAM or a VPC is. ' +
    'Their agent uses the same commands a developer would.',
  steps: [
    { title: 'Describe', body: 'In their own words, to the AI they already use' },
    { title: 'The agent builds', body: 'Claude Code, Cursor, Codex, Lovable…' },
    { title: 'Deploy', body: 'One command. Address, sign-in and database included' },
    { title: 'Share', body: 'With people and teams from your company’s sign-in' },
    { title: 'Iterate', body: 'Every change is a release; rollback in one step' },
  ],
  window: '~/expenses',
  replay: 'Replay',
  // The conversation the window plays, each message under the step it
  // belongs to. What the agent runs are its steps.
  messages: [
    { step: 0, from: 'person', text: 'Build an app to approve expense claims for the finance team.' },
    { step: 1, from: 'agent', text: 'Built it: a Next.js app with a Postgres database, 14 files. It runs at http://localhost:3000.' },
    { step: 2, from: 'person', text: 'Put it online for the company.' },
    {
      step: 2,
      from: 'agent',
      text: 'It’s live at https://acme-expenses.shpyrd.app, behind your company’s sign-in.',
      runs: ['shpyrd pg create db --project expenses', 'shpyrd deploy'],
    },
    { step: 3, from: 'person', text: 'Share it with the finance team.' },
    {
      step: 3,
      from: 'agent',
      text: 'Done: Finance can open it, and will find it among their apps.',
      runs: ['shpyrd members add expenses --team finance --role user'],
    },
    { step: 4, from: 'person', text: 'The totals are wrong. Put the last version back.' },
    { step: 4, from: 'agent', text: 'Version 13 is live again. Want me to look at the totals?', runs: ['shpyrd rollback'] },
  ],
}

// Who gets in: the same app, three people.
export const access = {
  label: 'Who gets in',
  heading: 'Sign-in the app never had to write',
  lead:
    'The same app, three people. The front door decides who reaches it, and ' +
    'tells the app who they are: it reads a header, not a password table.',
  app: 'expenses',
  url: 'https://acme-expenses.shpyrd.app',
  people: [
    {
      id: 'finance',
      name: 'Marina, Finance',
      email: 'marina@yourcompany.com',
      verdict: 'Signed in with the company account. Finance is on the app: she gets in.',
      allowed: true,
    },
    {
      id: 'marketing',
      name: 'Diego, Marketing',
      email: 'diego@yourcompany.com',
      verdict: 'Signed in, but Marketing isn’t on the app: turned away at the door.',
      allowed: false,
    },
    {
      id: 'outside',
      name: 'Someone outside',
      email: 'someone@gmail.com',
      verdict: 'Not one of yours: the app never sees the request.',
      allowed: false,
    },
  ],
  policyLabel: 'shpyrd.yaml and roles',
  headersLabel: 'What the app receives',
  denied: 'You don’t have access to expenses',
  deniedHint: 'Ask its owner to share it with you.',
}

// Sleep: what idle costs, and what it doesn't.
export const sleep = {
  label: 'Sleep and scale',
  heading: ['Internal apps sit idle most of the day.', 'Pay for what runs.'],
  lead:
    'Apps and databases sleep when nobody uses them, wake on the first visit ' +
    'in seconds, and add instances at the peak. Nobody configures a thing.',
  legend: { users: 'people online', instances: 'instances on shpyrd', always: 'always on, sized for the peak' },
  hour: 'Time',
  online: 'People online',
  state: 'Instances',
  asleep: 'asleep',
  apps: 'Apps',
  databases: 'Databases sleep too',
  alwaysOn: 'Always on',
  onShpyrd: 'On shpyrd',
  saving: 'less on infrastructure a month',
  note: 'an illustrative estimate',
}

// Where it runs: shpyrd cloud first, your own cloud next.
export const delivery = {
  label: 'Where it runs',
  heading: 'On shpyrd cloud, or in your own',
  lead:
    'The same experience for whoever builds. What changes is where the ' +
    'platform, and your data, live.',
  options: [
    {
      id: 'cloud',
      name: 'shpyrd cloud',
      points: [
        'Start today: connect your sign-in and ship the first app',
        'We run, update and scale the platform',
        'Private network applications and VPC peering to reach your data',
        'Pay for what runs, nothing for idle',
      ],
    },
    {
      id: 'own',
      name: 'Your own cloud',
      points: [
        'shpyrd in your own Kubernetes, on your account',
        'Your data never leaves your network',
        'Open source: the code is public, and yours to read',
        'A license switches on the enterprise features',
      ],
    },
  ],
  company: 'Your company',
  provider: 'Your sign-in (Entra · Google · Okta)',
  systems: 'Your systems',
  platformBox: 'shpyrd + your apps',
  databases: 'A database per app',
  link: 'private connection · encrypted',
  ownZone: 'Your cloud',
  ownLink: 'traffic and data stay inside your network',
}

// For IT and security: the catalogue and the trail.
export const governance = {
  label: 'For IT and security',
  heading: 'Freedom to build. Everything in view.',
  lead:
    'One list of every app: who owns it, which team, who can open it, what ' +
    'it costs, and a trail of every change.',
  stats: [
    { value: '47', label: 'apps in one list' },
    { value: '100%', label: 'behind your sign-in' },
    { value: '31', label: 'asleep right now' },
    { value: '0', label: 'secrets in the code' },
  ],
  columns: ['App', 'Owner', 'People', 'Cost / month', 'State'],
  rows: [
    { app: 'expenses', team: 'Finance', owner: 'marina@', people: 23, cost: '$4.10', awake: true },
    { app: 'pipeline', team: 'Sales', owner: 'diego@', people: 41, cost: '$6.80', awake: true },
    { app: 'onboarding', team: 'People', owner: 'lucas@', people: 8, cost: '$0.90', awake: false },
    { app: 'campaigns', team: 'Marketing', owner: 'bia@', people: 15, cost: '$2.40', awake: true },
    { app: 'inventory', team: 'Ops', owner: 'rafa@', people: 12, cost: '$1.20', awake: false },
    { app: 'contracts', team: 'Legal', owner: 'julia@', people: 5, cost: '$0.60', awake: false },
  ],
  trail: 'audit trail',
  // The trail plays these, newest on top, one every few seconds.
  events: [
    'marina@ deployed expenses v14',
    'diego@ was turned away at contracts',
    'bia@ shared campaigns with team:marketing',
    'onboarding fell asleep after 15 min idle',
    'rafa@ rolled back inventory to v6',
    'julia@ set a new secret on contracts',
    'pipeline woke up for diego@',
  ],
}

// Who wins.
export const audiences = {
  label: 'Who it’s for',
  heading: 'Everybody wins, even whoever says “no”',
  lead: 'The platform takes IT out of the way without taking IT out of control.',
  groups: [
    {
      name: 'Builders, in every team',
      quote: '“I just want the app to work and my team to use it.”',
      points: ['Ship without a ticket', 'Sign-in and permissions already done', 'One sentence to share it'],
    },
    {
      name: 'IT and security',
      quote: '“I can’t block everything, but I can’t be blind either.”',
      points: ['Company sign-in, roles and isolation on every app', 'One list and one audit trail', 'The end of shadow IT'],
    },
    {
      name: 'Engineering',
      quote: '“I don’t want to babysit 40 apps someone else wrote.”',
      points: ['No platform to build or run', 'Logs, metrics and rollback for each app', 'Any stack, from the repository'],
    },
    {
      name: 'Finance',
      quote: '“Why are 50 databases on at three in the morning?”',
      points: ['Apps and databases that sleep', 'Cost per app and per team', 'No platform team to hire'],
    },
  ],
}

export const closing = {
  heading: ['Let everyone build.', 'We’ll run the rest.'],
  lead:
    'Cheap, fast and safe: sign-in, roles, isolation, backups and sleep, by ' +
    'default, for every app.',
  points: ['shpyrd cloud or your own', 'Any AI agent', 'No rewrites'],
  pilot: {
    title: 'A 30-day pilot',
    weeks: [
      { title: 'Connect your sign-in', body: 'Your provider, its groups as teams, your domain' },
      { title: 'Move 3 apps you already have', body: 'No rewrite: built from the repository as it is' },
      { title: 'Open it to 2 teams', body: 'With the AI agents they already use' },
      { title: 'Measure', body: 'Cost, access and adoption, app by app' },
    ],
  },
  contact: 'Talk to us',
}

// The slides, in order: the name the deck's foot shows for each.
export const slides = [
  'shpyrd',
  'The problem',
  'X-ray',
  'The platform',
  'How it works',
  'Who gets in',
  'Sleep and scale',
  'Where it runs',
  'For IT and security',
  'Who it’s for',
  'Next steps',
]
