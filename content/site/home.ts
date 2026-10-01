// The homepage: what the builder built, what we do with it, the questions that
// keep an app on a laptop, the way from the laptop to the team, and the other
// people who choose shpyrd. The page draws these in that order
// (apps/website/src/components/home.tsx); the icons are the page's, matched by id.

export const hero = {
  heading: 'You built it. We ship it.',
  description:
    'AI got your app working. We take it from there: we put it online behind a ' +
    'sign-in, and give it everything it needs to run, from its database to its backups.',
}

export const gap = {
  label: 'The part nobody plans for',
  heading: 'Working and used are two different things',
  description:
    'Building the app was the part you knew how to ask for. The questions after it ' +
    'are the ones that keep it on your laptop.',
  items: [
    {
      id: 'where',
      title: 'Where does it live?',
      body:
        'Not on your laptop, and not on a free host under your personal card. On shpyrd ' +
        'cloud, or on a cluster your company controls, at an address your colleagues can open.',
    },
    {
      id: 'who',
      title: 'Who can open it?',
      body:
        'Finance and Operations, after signing in with their company account. Everyone ' +
        'else meets a sign-in page, not your app.',
    },
    {
      id: 'break',
      title: 'What if I break it?',
      body:
        'Every change is a numbered release. When an update breaks something people ' +
        'depend on, go back to the one before.',
    },
  ],
}

export const route = {
  heading: 'From your laptop to your team',
  description: 'The same agent that built the app takes it the rest of the way.',
  steps: [
    {
      id: 'connect',
      title: 'Connect your agent to your workspace.',
      body: 'One line in Claude Code, Codex or Cursor.',
    },
    {
      id: 'deploy',
      title: 'Deploy the app.',
      body: 'It gets an address, TLS, and a sign-in in front of it. Nobody gets in yet.',
    },
    {
      id: 'share',
      title: 'Share it with the people it is for.',
      body: 'A team, or named colleagues. They find it among their apps the next time they sign in.',
    },
    {
      id: 'change',
      title: 'Keep changing it.',
      body:
        'Each update is a release your colleagues get without asking; a bad one is undone ' +
        'in one step.',
    },
  ],
}

export const also = {
  label: 'Also for',
  heading: 'Not the one who built it?',
  items: [
    {
      href: '/for/developers',
      title: 'Developers',
      body: 'A CLI, releases and rollback, on shpyrd cloud or on your own cluster.',
    },
    {
      href: '/for/it',
      title: 'IT teams',
      body: 'One accepted place for the apps your people build, behind your sign-in.',
    },
    {
      href: '/for/fde-partners',
      title: 'FDE partners',
      body: 'The same setup behind every client delivery, and a clean handover.',
    },
  ],
}

export const boundariesSection = {
  title: "What this does and doesn't do",
  intro:
    'shpyrd is in beta and developed in the open. The limits below are the ' +
    'ones worth knowing before you spend time on an evaluation.',
}
