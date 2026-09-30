// The homepage, in the order the positioning research argues for: the outcome,
// then a situation the reader recognises, then what they get, then where it
// runs, then what goes in it, then the offer, then the limits.

export const situation = {
  title: 'The part nobody plans for',
  body: [
    'Say your operations team has built a purchase-request tracker. It fits how ' +
      'the team works, and the first version works. Now Finance needs access. ' +
      'Someone has to decide where it runs. And somebody will be responsible ' +
      'for the next update.',
    'shpyrd is where that app goes next.',
  ],
}

export const pillars = {
  title: 'Three things you get',
  items: [
    {
      id: 'available',
      title: 'Make it available.',
      body:
        'Deploy a compatible app from the code your team already has. It gets ' +
        'a URL, TLS, and sign-in in front of it.',
    },
    {
      id: 'access',
      title: 'Choose who can use it.',
      body:
        'Share an app with Finance, Operations or another selected group — and ' +
        'keep permission to use an app separate from permission to update or ' +
        'administer it.',
    },
    {
      id: 'manage',
      title: 'Manage it over time.',
      body:
        'Every deploy, config change and rollback is a numbered release. When ' +
        'an update breaks something people depend on, restore the previous one.',
    },
  ],
}

export const hosting = {
  title: 'Where it runs, and who runs it',
  body: [
    'shpyrd runs on a Kubernetes cluster your company controls — locally on ' +
      'kind, on Oracle Cloud (OKE), or on AWS (EKS).',
    'Someone has to own that cluster. During the beta, that is a conversation ' +
      'we have with you rather than a box you tick.',
  ],
  link: { label: 'Read the deployment docs', href: '/docs/installation' },
}

export const useCases = {
  title: 'What people put in it',
  // What a colleague sees when they sign in: the apps, and who each is for.
  // The last row is the hook - it is the reader's own app, the one that works
  // and that nobody else can open yet.
  caption: 'Your workspace',
  apps: [
    { name: 'Purchase requests', audience: 'Finance, Operations', shared: true },
    { name: 'Onboarding checklist', audience: 'People', shared: true },
    { name: 'Quote tool', audience: 'Sales', shared: true },
    { name: 'Field reports', audience: 'Not shared yet', shared: false },
  ],
  disclaimer:
    "shpyrd doesn't build these. Your team does, with the tools it already " +
    'uses. shpyrd runs them, decides who gets in, and gives colleagues one ' +
    'place to find them.',
}

export const boundariesSection = {
  title: "What this does and doesn't do",
  intro:
    'shpyrd is in beta and developed in the open. The limits below are the ' +
    'ones worth knowing before you spend time on an evaluation.',
}

export const developerSection = {
  title: 'Run it yourself',
  body:
    'shpyrd is open source under MPL-2.0. The whole base stack installs onto a ' +
    'local kind cluster from one command, and the quick start takes you from ' +
    'nothing to a deployed app.',
  code: `brew install shpyrd-io/tap/shpyrd
shpyrd cluster create
shpyrd cluster dashboard`,
  links: [
    { label: 'Quick start', href: '/docs/getting-started' },
    { label: 'GitHub', href: 'https://github.com/shpyrd-io/shpyrd' },
    { label: 'Discord', href: 'https://discord.gg/AxWMXXW7' },
  ],
}
