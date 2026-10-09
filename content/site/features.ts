import type { Landing } from './landings'

// The Features menu and its six pages, from "Shpyrd Features" (arquivos de
// copy, selected-features.md, 2026-10-09). Each page: the outcome, an
// example, the capabilities that make it possible, the practical questions,
// one next step. Claims stay inside what that brief establishes: no uptime
// figures, recovery times, backup frequencies or savings that have not been
// set for the offer.

export const features: Landing[] = [
  {
    slug: 'easy-deployment',
    name: 'Easy Deployment',
    card: 'Easily deploy your project and keep publishing improvements.',
    icon: 'rocket',
    label: 'Easy Deployment',
    heading: 'Easily deploy your project.',
    description:
      'Deploy from your project, a Git repository, or a ready-to-run image. Publish updates through the same workflow as your project evolves.',
    primary: { label: 'Deploy a project', href: 'signup' },
    secondary: { label: 'Read the deployment guide', href: '/docs/deploying' },
    example: {
      heading: 'Publish it. Open it. Publish the next version.',
      description: 'An existing site or app, from the first deployment to the next change.',
      steps: [
        { icon: 'rocket', heading: 'Deploy the project', body: 'From your project folder, a Git repository, or a container image. The build runs as part of the deployment.' },
        { icon: 'globe', heading: 'Open it', body: 'The app gets its address, and you can use your domain.' },
        { icon: 'refresh', heading: 'Publish the next version', body: 'Make a change and deploy again. Each deployment is recorded as a release.' },
      ],
    },
    capabilities: {
      heading: 'What makes it work',
      items: [
        { icon: 'code', heading: 'Code, Git, or an image', body: 'Deploy local project code, a Git repository, or a container image.' },
        { icon: 'package', heading: 'Build in the workflow', body: 'Supported projects are detected and built as part of the deployment, with build configuration when you need it.' },
        { icon: 'history', heading: 'Every deployment a release', body: 'Later deployments are recorded as releases you can review.' },
        { icon: 'terminal', heading: 'CLI and API', body: 'The same workflow works from the CLI and the API, for developers, scripts, and agents.' },
        { icon: 'globe', heading: 'Use your domain', body: 'Every app gets an address. Use your domain, such as acme.com or app.acme.com.' },
        { icon: 'bot', heading: 'Ask your agent', body: 'An agent can deploy through the authenticated CLI. Tell it: "Publish this app."' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does every project deploy without configuration?', a: 'Supported projects are detected and built for you. Some projects need build configuration; the deployment guide explains the options.' },
        { q: 'Can my AI agent deploy for me?', a: 'Yes, through the authenticated CLI or API. The workspace MCP connection is for inspecting projects, logs, and metrics, not for deploying.' },
        { q: 'Can I use my domain?', a: 'Use your domain.' },
      ],
    },
    related: [
      { label: 'Apps Built with AI', href: '/solutions/apps-built-with-ai' },
      { label: 'Websites & Landing Pages', href: '/solutions/websites' },
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
    ],
    close: {
      heading: 'Which project should we put online?',
      description: 'Deploy it, open it, and publish the next version from the same workflow.',
      action: { label: 'Deploy a project', href: 'signup' },
    },
  },
  {
    slug: 'fully-managed-cloud',
    name: 'Fully Managed Cloud',
    card: 'Fully managed, fault-tolerant cloud with complete backups. We take care of the infrastructure.',
    icon: 'cloud',
    label: 'Fully Managed Cloud',
    heading: 'We take care of the infrastructure.',
    description:
      'Run your projects on fully managed, fault-tolerant cloud with complete backups. Keep building and improving your applications while we handle the infrastructure.',
    primary: { label: 'Get started on Shpyrd Cloud', href: 'signup' },
    secondary: { label: 'See pricing', href: '/pricing' },
    example: {
      heading: 'You keep developing. We keep it running.',
      description: 'A real application and its saved information, and who takes care of what.',
      steps: [
        { icon: 'rocket', heading: 'Your app runs on Shpyrd Cloud', body: 'Deploy it and use it. There is no environment for you to set up or maintain.' },
        { icon: 'shield', heading: 'We operate the infrastructure', body: 'Fault tolerance and infrastructure operations are part of the Cloud service.' },
        { icon: 'archive', heading: 'Backups cover apps and data', body: 'Complete backups, with recovery as part of the managed service.' },
      ],
    },
    capabilities: {
      heading: 'What the service includes',
      items: [
        { icon: 'cloud', heading: 'Fully managed', body: 'Infrastructure operations are managed by Shpyrd.' },
        { icon: 'shield', heading: 'Fault tolerant', body: 'Fault tolerance is part of the Cloud service.' },
        { icon: 'archive', heading: 'Complete backups', body: 'Backups cover applications and data, with recovery as part of the managed service.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'What does "managed" cover?', a: 'The infrastructure your applications run on. Your application code, its business logic, and the external services you choose stay yours.' },
        { q: 'Is restoring a backup the same as rolling back?', a: 'No. Rolling back returns your application code and configuration to an earlier release. Recovering database records and files uses the backups.' },
        { q: 'What if we need to run it in our own environment?', a: 'You can self-host, with support from the community, including Discord. An Enterprise contract adds Shpyrd support and an SLA in your infrastructure; operating it stays with you or your operator.' },
      ],
    },
    related: [
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
      { label: 'Managed White Label Infrastructure', href: '/solutions/white-label-infrastructure' },
    ],
    close: {
      heading: 'Keep creating. We take care of the hosting.',
      description: 'Put your project on Shpyrd Cloud and leave the infrastructure to us.',
      action: { label: 'Get started on Shpyrd Cloud', href: 'signup' },
    },
  },
  {
    slug: 'apps-and-databases',
    name: 'Apps & Databases Together',
    card: 'Run your app, database, and background processes in one place.',
    icon: 'database',
    label: 'Apps & Databases Together',
    heading: 'Your app, database, and background processes. In one place.',
    description:
      'Deploy your application or API, connect PostgreSQL, and add the background processes and persistent storage your project needs. Keep the application\'s resources together as it grows.',
    primary: { label: 'Deploy your app and connect a database', href: 'signup' },
    secondary: { label: 'Read about databases', href: '/docs/databases' },
    example: {
      heading: 'A booking app, its data, and its background work.',
      description: 'Each part does a job the app needs.',
      steps: [
        { icon: 'layout', heading: 'The app takes a booking', body: 'A web app or API serves the request.' },
        { icon: 'database', heading: 'PostgreSQL keeps it', body: 'The booking is saved and read back later.' },
        { icon: 'cpu', heading: 'A worker does the rest', body: 'A background process handles a task, such as sending a document to an external AI service.' },
      ],
    },
    capabilities: {
      heading: 'What a project can run',
      items: [
        { icon: 'layout', heading: 'Web apps and APIs', body: 'Your application, with or without a browser interface.' },
        { icon: 'cpu', heading: 'Background workers', body: 'Processes that do work outside a request.' },
        { icon: 'database', heading: 'PostgreSQL', body: 'A database for the information your app saves.' },
        { icon: 'layers', heading: 'Redis or Valkey', body: 'For compatible caching and queue use cases.' },
        { icon: 'hard-drive', heading: 'Persistent storage', body: 'Volumes for the files your application keeps.' },
        { icon: 'gauge', heading: 'Adjustable capacity', body: 'Connections, configuration, and sizes managed with the project.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does a simple website need a database?', a: 'No. Add a database when your app needs to save information.' },
        { q: 'Does capacity grow with traffic on its own?', a: 'Capacity is adjustable: you set the process counts and resource sizes your project needs.' },
        { q: 'Does Shpyrd provide the booking or payment logic?', a: 'No. Shpyrd runs your application and its supported resources. Your code and the services you choose provide the business functionality.' },
      ],
    },
    related: [
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'AI Apps & Agents', href: '/solutions/ai-apps-and-agents' },
      { label: 'Internal Apps', href: '/solutions/internal-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
    ],
    close: {
      heading: 'A place for your app and its data.',
      description: 'Deploy the application, connect PostgreSQL, and add the worker it needs.',
      action: { label: 'Deploy your app and connect a database', href: 'signup' },
    },
  },
  {
    slug: 'app-access',
    name: 'Built-in App Access',
    card: 'Make your app public or choose who can use it. Connect your company\'s login when needed.',
    icon: 'lock',
    label: 'Built-in App Access',
    heading: 'Choose who can use each app.',
    description:
      'Make an app public or restrict access to selected people and teams. Connect your company\'s login and give users one place to find the applications they can use.',
    primary: { label: 'Put an app to work with the right access', href: 'signup' },
    secondary: { label: 'Read about app access', href: '/docs/app-access' },
    example: {
      heading: 'The right people open the right apps.',
      description: 'An app restricted to a team, and a site anyone can open.',
      steps: [
        { icon: 'key', heading: 'Sign in with your company\'s login', body: 'A person signs in through single sign-on (SSO), where it is set up.' },
        { icon: 'layout', heading: 'Open the app from the portal', body: 'The portal lists the apps that person may open.' },
        { icon: 'globe', heading: 'Or open it to everyone', body: 'A public site needs no invitation: visitors just open it.' },
      ],
    },
    capabilities: {
      heading: 'What makes it work',
      items: [
        { icon: 'globe', heading: 'Public or sign-in required', body: 'Choose for each application.' },
        { icon: 'key', heading: 'Company sign-in', body: 'Through supported identity providers.' },
        { icon: 'users', heading: 'People, teams, and roles', body: 'Separate permissions to use an app, view its management information, publish changes, and administer.' },
        { icon: 'layout', heading: 'An app portal', body: 'One place where people find the apps they can use.' },
        { icon: 'eye', heading: 'Identity for your app', body: 'Who is signed in is made available to the application, for supported integrations.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does access control what people can do inside the app?', a: 'No. Shpyrd controls who can open the app and tells it who they are. Actions inside it, like approving a payment, are your app\'s to define.' },
        { q: 'Can we use our company login?', a: 'We can confirm SSO support for your identity provider and the offer that fits your setup.' },
        { q: 'Can an app be open to the public?', a: 'Yes. Make it public and anyone with the link can open it.' },
      ],
    },
    related: [
      { label: 'Internal Apps', href: '/solutions/internal-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
    ],
    close: {
      heading: 'Use your company\'s login. Give people the apps they need.',
      description: 'Make an app public, or choose the people and teams who can use it.',
      action: { label: 'Put an app to work with the right access', href: 'signup' },
    },
  },
  {
    slug: 'automatic-sleep',
    name: 'Automatic Sleep & Wake',
    card: 'Reduce idle compute costs with automatic sleep and wake for apps and databases.',
    icon: 'moon',
    label: 'Automatic Sleep & Wake',
    heading: 'Reduce costs when your apps are idle.',
    description:
      'Let apps and databases sleep during periods of inactivity and wake when needed. Reduce idle compute costs while keeping their stored data.',
    primary: { label: 'Explore automatic sleep for your project', href: 'signup' },
    secondary: { label: 'See pricing', href: '/pricing' },
    example: {
      heading: 'Quiet for a while. Ready when someone comes back.',
      description: 'An app with a realistic period of inactivity.',
      steps: [
        { icon: 'moon', heading: 'Nobody uses it, it sleeps', body: 'Eligible web processes and databases sleep while idle.' },
        { icon: 'activity', heading: 'Someone opens it, it wakes', body: 'A request wakes the app; a new connection wakes the database.' },
        { icon: 'database', heading: 'The data is still there', body: 'Stored data is kept while compute sleeps.' },
      ],
    },
    capabilities: {
      heading: 'What makes it work',
      items: [
        { icon: 'layout', heading: 'Apps sleep and wake', body: 'Eligible web processes wake on an incoming request.' },
        { icon: 'database', heading: 'Databases too', body: 'Eligible PostgreSQL databases wake on a new connection.' },
        { icon: 'wrench', heading: 'Policies and controls', body: 'For the supported environment and offer.' },
        { icon: 'hard-drive', heading: 'Data kept', body: 'Persistent data stays while compute sleeps.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Is an idle app free?', a: 'Sleep reduces idle compute. Storage and other applicable charges continue, and plan minimums may apply.' },
        { q: 'How long does waking take?', a: 'Waking can take a moment, so the first request after a pause is slower than the next ones.' },
        { q: 'Can every worker and database sleep?', a: 'It depends on the component and its configuration. Database sleep applies to supported single-instance setups, and active connections keep a database awake.' },
      ],
    },
    related: [
      { label: 'Apps Built with AI', href: '/solutions/apps-built-with-ai' },
      { label: 'Internal Apps', href: '/solutions/internal-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
    ],
    close: {
      heading: 'Keep occasional-use apps available.',
      description: 'Without keeping all their compute running all the time.',
      action: { label: 'Explore automatic sleep for your project', href: 'signup' },
    },
  },
  {
    slug: 'project-management',
    name: 'Project Management & Visibility',
    card: 'See how your projects are running, publish improvements, and return to an earlier release when needed.',
    icon: 'activity',
    label: 'Project Management & Visibility',
    heading: 'Keep your projects easy to update and manage.',
    description:
      'Follow logs and metrics, review releases, and adjust capacity in one place. Publish the next version and roll back application code and configuration when needed.',
    primary: { label: 'Try it with a project and its next update', href: 'signup' },
    secondary: { label: 'Read about logs', href: '/docs/logs' },
    example: {
      heading: 'See it running. Change it. Go back if you need to.',
      description: 'An app receiving traffic, its next release, and the one before.',
      steps: [
        { icon: 'scroll', heading: 'Follow logs and metrics', body: 'See how the app is running and what it is doing.' },
        { icon: 'rocket', heading: 'Publish a change', body: 'Each deployment and configuration change becomes a release.' },
        { icon: 'history', heading: 'Roll back if needed', body: 'Return application code and configuration to an earlier release.' },
      ],
    },
    capabilities: {
      heading: 'What makes it work',
      items: [
        { icon: 'scroll', heading: 'Build and app logs', body: 'Follow what happens as it builds and runs.' },
        { icon: 'gauge', heading: 'Status and metrics', body: 'Resource usage and supported traffic, latency, and errors.' },
        { icon: 'history', heading: 'Releases and rollback', body: 'Each release with its configuration, and a way back to an earlier one.' },
        { icon: 'activity', heading: 'Health checks', body: 'See deployment progress, and why one failed.' },
        { icon: 'layers', heading: 'Adjustable capacity', body: 'Change supported process counts and resource sizes.' },
        { icon: 'bot', heading: 'CLI, API, and MCP', body: 'Your agent can inspect status, logs, and metrics; supported changes use the CLI or API.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does rolling back restore my data?', a: 'No. Rollback returns application code and configuration. Database records and files are recovered from backups.' },
        { q: 'Can my agent help me find a problem?', a: 'Yes. Through MCP it can inspect your project\'s status, logs, and metrics, and explain what it sees.' },
        { q: 'Does it scale with traffic on its own?', a: 'Capacity is adjustable: you choose the process counts and resource sizes.' },
      ],
    },
    related: [
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'AI Apps & Agents', href: '/solutions/ai-apps-and-agents' },
      { label: 'Internal Apps', href: '/solutions/internal-apps' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
    ],
    close: {
      heading: 'Keep publishing improvements.',
      description: 'See how your app is running, and return to an earlier version when you need to.',
      action: { label: 'Try it with a project and its next update', href: 'signup' },
    },
  },
]
