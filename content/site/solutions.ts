import type { Landing } from './landings'

// The Solutions menu and its twelve pages, in three groups (Giovani,
// 2026-10-09): who it is for (the five personas of the copy guidelines,
// 07-copy-and-messaging-guidelines.md), what you publish and run, and
// delivering for clients (both from selected-solutions.md). Each page keeps
// to its own decision, so no two read the same.

const who: Landing[] = [
  {
    slug: 'ai-community',
    name: 'AI Community',
    card: 'You create with AI. Shpyrd puts it online.',
    icon: 'sparkles',
    label: 'For the AI community',
    heading: 'You create with AI. Shpyrd puts it online.',
    description: 'Give your app a link people can open. Keep creating with the agent you already use.',
    primary: { label: 'Try it with an app you have already started', href: 'signup' },
    secondary: { label: 'Bring your project and let\'s look at it together', href: 'contact' },
    example: {
      heading: 'From your agent to a link people can open.',
      description: 'An agent that performs a task. A generator that saves someone time. A simulator that helps someone decide.',
      steps: [
        { icon: 'bot', heading: 'Tell your agent', body: '"Publish this app."' },
        { icon: 'link', heading: 'Share the link', body: 'Other people open it and use what you made.' },
        { icon: 'refresh', heading: 'Keep improving it', body: 'Publish the next version whenever it is ready.' },
      ],
    },
    capabilities: {
      heading: 'What you get',
      items: [
        { icon: 'link', heading: 'A link people can open', body: 'Your app on the internet, ready to try.' },
        { icon: 'database', heading: 'A place for its data', body: 'Let your app save information when it needs to.' },
        { icon: 'history', heading: 'A way back', body: 'Go back to an earlier version of the app if a change goes wrong.' },
      ],
    },
    questions: {
      heading: 'Questions people ask',
      items: [
        { q: 'How can other people use what I made?', a: 'Publish it with Shpyrd and give them a link they can open.' },
        { q: 'Do I need to know about servers?', a: 'No. Keep creating with your agent. Use Shpyrd to put the project online.' },
        { q: 'Can I use my domain?', a: 'Use your domain.' },
      ],
    },
    related: [
      { label: 'Apps Built with AI', href: '/solutions/apps-built-with-ai' },
      { label: 'Easy Deployment', href: '/features/easy-deployment' },
      { label: 'Fully Managed Cloud', href: '/features/fully-managed-cloud' },
    ],
    close: {
      heading: 'Which idea of yours deserves a place on the internet?',
      description: 'Pick something you have already made and put it online.',
      action: { label: 'Try it with an app you have already started', href: 'signup' },
    },
  },
  {
    slug: 'vibe-coders',
    name: 'Vibe Coders',
    card: 'Your site or app, online.',
    icon: 'pencil',
    label: 'For vibe coders',
    heading: 'Your site or app, online.',
    description: 'Shpyrd puts the project you created with AI on the internet. You keep creating. People get a link they can open.',
    primary: { label: 'Put your project online', href: 'signup' },
    secondary: { label: 'See pricing', href: '/pricing' },
    example: {
      heading: 'You made the site. Now give people a link.',
      description: 'Your portfolio, a useful tool, or the first version of a product.',
      steps: [
        { icon: 'rocket', heading: 'Put it online', body: 'Your site or app goes on the internet.' },
        { icon: 'link', heading: 'Send the link', body: 'People open it and use it.' },
        { icon: 'refresh', heading: 'Publish the next version', body: 'When it is ready, put it online the same way.' },
      ],
    },
    capabilities: {
      heading: 'What you get',
      items: [
        { icon: 'globe', heading: 'Use your domain', body: 'Your site or app at your own address.' },
        { icon: 'database', heading: 'Save information', body: 'Add a database when your app needs to save information.' },
        { icon: 'history', heading: 'Go back', body: 'Return to an earlier version if a change goes wrong.' },
      ],
    },
    questions: {
      heading: 'Questions people ask',
      items: [
        { q: 'Can I use my domain?', a: 'Use your domain.' },
        { q: 'Can it be a real product, not just a demo?', a: 'Yes. Put your site, tool, or product online and keep publishing improvements as you go.' },
        { q: 'What does it cost?', a: 'See the current plans and their limits on the pricing page.' },
      ],
    },
    related: [
      { label: 'Websites & Landing Pages', href: '/solutions/websites' },
      { label: 'Apps Built with AI', href: '/solutions/apps-built-with-ai' },
      { label: 'Easy Deployment', href: '/features/easy-deployment' },
    ],
    close: {
      heading: 'Your next link could be your product.',
      description: 'Choose what you want to launch and put it online.',
      action: { label: 'Put your project online', href: 'signup' },
    },
  },
  {
    slug: 'developers',
    name: 'Developers',
    card: 'Easily deploy your project.',
    icon: 'code',
    label: 'For developers',
    heading: 'Easily deploy your project.',
    description: 'Keep deployments, databases, and operation in one workflow, so you can focus on developing the project. Whether it is for you, a client, or your company.',
    primary: { label: 'Deploy a project', href: 'signup' },
    secondary: { label: 'Read the getting-started guide', href: '/docs/getting-started' },
    example: {
      heading: 'The first deployment, the next change, the release history.',
      description: 'Try it with a project you are already developing.',
      steps: [
        { icon: 'rocket', heading: 'Deploy', body: 'From local code, Git, or a container image.' },
        { icon: 'database', heading: 'Connect what it needs', body: 'PostgreSQL, Valkey, workers, and volumes in the same project.' },
        { icon: 'history', heading: 'Release, inspect, roll back', body: 'Follow the logs, review releases, and roll back code and configuration.' },
      ],
    },
    capabilities: {
      heading: 'What comes with it',
      items: [
        { icon: 'cpu', heading: 'Web and worker processes', body: 'Keep web processes, workers, and connected resources in the same project.' },
        { icon: 'scroll', heading: 'Logs and metrics', body: 'Build and application logs, resource and traffic metrics.' },
        { icon: 'terminal', heading: 'CLI and API', body: 'Script it, or let your agent drive the CLI.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'This is a client project.', a: 'That fits. Shpyrd can support the publication and updates of applications you build for clients.' },
        { q: 'I already have hosting.', a: 'Try Shpyrd with one project and compare the workflow from the first deployment through the next update.' },
        { q: 'Does rollback undo database migrations?', a: 'No. Rollback returns application code and configuration; it does not reverse migrations or saved data.' },
      ],
    },
    related: [
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'Easy Deployment', href: '/features/easy-deployment' },
      { label: 'Apps & Databases Together', href: '/features/apps-and-databases' },
      { label: 'Project Management & Visibility', href: '/features/project-management' },
    ],
    close: {
      heading: 'Which project should we put online?',
      description: 'Deploy it, then publish a second version and evaluate the workflow.',
      action: { label: 'Deploy a project', href: 'signup' },
    },
  },
  {
    slug: 'businesses',
    name: 'Businesses',
    card: 'Put your business apps to work.',
    icon: 'building',
    label: 'For businesses',
    heading: 'Put your business apps to work.',
    description: 'Publish the applications your customers and teams need, keep them updated, and choose the support that fits your operation.',
    primary: { label: 'Start with an app', href: 'signup' },
    secondary: { label: 'Explore the right option for your company', href: 'contact' },
    example: {
      heading: 'One app in use, the right people on it.',
      description: 'Your site, a customer app, or an internal tool.',
      steps: [
        { icon: 'rocket', heading: 'Get the app running', body: 'Put it to use and use your company\'s domain.' },
        { icon: 'key', heading: 'Use your company\'s login', body: 'Choose who can use, publish, and manage each app.' },
        { icon: 'refresh', heading: 'Keep it updated', body: 'Publish improvements as your needs change.' },
      ],
    },
    capabilities: {
      heading: 'What matters to your business',
      items: [
        { icon: 'users', heading: 'Access and permissions', body: 'Choose who can use, publish, and manage each app.' },
        { icon: 'key', heading: 'Single sign-on', body: 'Use your company\'s login, with supported identity providers.' },
        { icon: 'coins', heading: 'Costs you can plan', body: 'Choose the plan that fits your apps and support needs.' },
        { icon: 'handshake', heading: 'Support that fits', body: 'Support arrangements that match how your business operates.' },
        { icon: 'building', heading: 'Your cloud, if you need it', body: 'Start on Shpyrd Cloud, or explore Shpyrd Enterprise in your own cloud.' },
        { icon: 'activity', heading: 'One place to manage', body: 'Status, logs, and releases for every app.' },
      ],
    },
    questions: {
      heading: 'Questions businesses ask',
      items: [
        { q: 'Do we need Enterprise?', a: 'Start with what your application needs. Enterprise is an option for specific requirements, not a prerequisite for using Shpyrd as a business.' },
        { q: 'Can we use our company login?', a: 'We can confirm SSO support for your identity provider and the offer that fits your setup.' },
        { q: 'Do we have to move everything?', a: 'Start with one application. Use that experience to decide whether Shpyrd fits more of your applications.' },
        { q: 'What support do we get?', a: 'The selected offer defines the support channels, coverage, and any response commitments. We can match those to your application\'s needs.' },
      ],
    },
    related: [
      { label: 'Internal Apps', href: '/solutions/internal-apps' },
      { label: 'Built-in App Access', href: '/features/app-access' },
      { label: 'Fully Managed Cloud', href: '/features/fully-managed-cloud' },
    ],
    close: {
      heading: 'Which app would you like to start with?',
      description: 'Get it running, give the right people access, and keep it updated.',
      action: { label: 'Start with an app', href: 'signup' },
    },
  },
  {
    slug: 'implementation-partners',
    name: 'FDEs & Implementation Partners',
    card: 'Simplify how you deploy and manage your clients\' projects.',
    icon: 'handshake',
    label: 'For FDEs & implementation partners',
    heading: 'Simplify how you deploy and manage your clients\' projects.',
    description: 'Give each client a working application and a clear path forward, whether you hand it over or continue managing it.',
    primary: { label: 'Try Shpyrd with a client project', href: 'signup' },
    secondary: { label: 'Explore managed infrastructure for your clients', href: '/solutions/white-label-infrastructure' },
    example: {
      heading: 'One client project, and what happens after launch.',
      description: 'Choose the model that fits each engagement.',
      steps: [
        { icon: 'handshake', heading: 'Implement and hand over', body: 'Deploy your client\'s project. Make it easy for them to take over, contracting directly with Shpyrd.' },
        { icon: 'wrench', heading: 'Implement and maintain', body: 'Keep your clients\' projects running and easy to update, while the client contracts for the infrastructure.' },
        { icon: 'tag', heading: 'Offer a managed service', body: 'Offer managed infrastructure under your own brand. Set your prices. Shpyrd handles the infrastructure.' },
      ],
    },
    capabilities: {
      heading: 'What helps the delivery',
      items: [
        { icon: 'rocket', heading: 'Deploy and update', body: 'Code, Git, or an image; every change a release.' },
        { icon: 'database', heading: 'Resources in one project', body: 'Keep the application and its connected resources together.' },
        { icon: 'users', heading: 'Access for you and your client', body: 'Set up access for your team and your client, with their login where needed.' },
      ],
    },
    questions: {
      heading: 'Questions partners ask',
      items: [
        { q: 'I only want to implement and hand it over.', a: 'That fits. Your client can contract directly with Shpyrd. We can define the deployment and handover that let them take over.' },
        { q: 'Can I maintain the app without reselling infrastructure?', a: 'Yes. You can continue developing and supporting the application while your client contracts for the infrastructure.' },
        { q: 'Does running in my client\'s cloud mean Shpyrd manages it?', a: 'The selected offer defines who operates the environment. Self-hosting and Shpyrd-managed infrastructure have different responsibilities.' },
      ],
    },
    related: [
      { label: 'Client Projects', href: '/solutions/client-projects' },
      { label: 'Managed White Label Infrastructure', href: '/solutions/white-label-infrastructure' },
      { label: 'Apps & Databases Together', href: '/features/apps-and-databases' },
    ],
    close: {
      heading: 'Which client project would you like to start with?',
      description: 'Deploy it, update it, and choose who runs it after delivery.',
      action: { label: 'Try Shpyrd with a client project', href: 'signup' },
    },
  },
]

const publish: Landing[] = [
  {
    slug: 'apps-built-with-ai',
    name: 'Apps Built with AI',
    card: 'Put the project you created with an agent online.',
    icon: 'bot',
    label: 'Apps Built with AI',
    heading: 'You built it with AI. Put it online.',
    description: 'Your project works on your computer. Put it on the internet, give people a link, and keep improving it with your agent.',
    primary: { label: 'Put your project online', href: 'signup' },
    secondary: { label: 'Read the getting-started guide', href: '/docs/getting-started' },
    example: {
      heading: 'A calculator, a generator, a portfolio. Online.',
      description: 'From a project that works to one other people can use.',
      steps: [
        { icon: 'bot', heading: 'Ask your agent', body: '"Publish this app." It deploys through the Shpyrd CLI.' },
        { icon: 'link', heading: 'Open the link', body: 'Your app is public, and you can use your domain.' },
        { icon: 'refresh', heading: 'Improve it', body: 'The next change reaches the published app as a new release.' },
      ],
    },
    capabilities: {
      heading: 'What you get',
      items: [
        { icon: 'globe', heading: 'Public by default', body: 'Anyone with the link can open it, and you can use your domain.' },
        { icon: 'history', heading: 'Every update a release', body: 'Go back to an earlier version if a change goes wrong.' },
        { icon: 'moon', heading: 'Sleeps when idle', body: 'Reduce idle compute while nobody is using it.' },
      ],
    },
    questions: {
      heading: 'Questions people ask',
      items: [
        { q: 'Does my app need AI features?', a: 'No. Any site or tool you made with an agent fits, whatever it does when it runs.' },
        { q: 'Can my agent publish it for me?', a: 'Yes, through the Shpyrd CLI. The workspace MCP connection lets your agent inspect the project, its logs, and metrics.' },
        { q: 'Can I import a project from an online builder?', a: 'Bring the project\'s code. Shpyrd publishes and runs it; it does not import a builder\'s own project format.' },
      ],
    },
    related: [
      { label: 'AI Community', href: '/solutions/ai-community' },
      { label: 'Vibe Coders', href: '/solutions/vibe-coders' },
      { label: 'Easy Deployment', href: '/features/easy-deployment' },
      { label: 'Automatic Sleep & Wake', href: '/features/automatic-sleep' },
    ],
    close: {
      heading: 'Give it a link people can open.',
      description: 'Put your project online and keep publishing improvements.',
      action: { label: 'Put your project online', href: 'signup' },
    },
  },
  {
    slug: 'websites',
    name: 'Websites & Landing Pages',
    card: 'Publish your site, use your domain, and keep it updated.',
    icon: 'globe',
    label: 'Websites & Landing Pages',
    heading: 'Your website, online.',
    description: 'Publish your portfolio, service site, event page, or product launch. Use your domain and keep it updated.',
    primary: { label: 'Put your website online', href: 'signup' },
    secondary: { label: 'Read about static sites', href: '/docs/deploying' },
    example: {
      heading: 'A launch page, live and easy to change.',
      description: 'Publish it, open it, and update it as plans change.',
      steps: [
        { icon: 'rocket', heading: 'Publish the site', body: 'Static files or a Vite build, served for visitors.' },
        { icon: 'globe', heading: 'Use your domain', body: 'Your site at your address.' },
        { icon: 'pencil', heading: 'Update it', body: 'Publish changes the same way, whenever you need to.' },
      ],
    },
    capabilities: {
      heading: 'What you get',
      items: [
        { icon: 'file', heading: 'Static sites and Vite', body: 'Static files served through nginx or httpd, and documented Vite builds.' },
        { icon: 'globe', heading: 'Open to visitors', body: 'Public access, with your domain.' },
        { icon: 'refresh', heading: 'Updates as releases', body: 'Each change is a release you can go back from.' },
      ],
    },
    questions: {
      heading: 'Questions people ask',
      items: [
        { q: 'Can my site have a form?', a: 'Yes, when the form is built into your site\'s code or a service it connects to.' },
        { q: 'Is there a site builder or CMS?', a: 'No. You build the site, with your agent or your tools; Shpyrd puts it online and keeps it updated.' },
        { q: 'My site needs a database.', a: 'Then it is an app: see Web Apps & APIs for apps with databases and workers.' },
      ],
    },
    related: [
      { label: 'Vibe Coders', href: '/solutions/vibe-coders' },
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'Easy Deployment', href: '/features/easy-deployment' },
    ],
    close: {
      heading: 'Your site is ready for visitors.',
      description: 'Put it online. Use your domain. Keep creating.',
      action: { label: 'Put your website online', href: 'signup' },
    },
  },
  {
    slug: 'web-apps',
    name: 'Web Apps & APIs',
    card: 'Deploy applications, APIs, and workers with the resources they need.',
    icon: 'server',
    label: 'Web Apps & APIs',
    heading: 'Easily deploy your project.',
    description: 'Run your web app or API with its database, workers, and storage, and deploy every change through one repeatable workflow.',
    primary: { label: 'Deploy a project', href: 'signup' },
    secondary: { label: 'Read about resources', href: '/docs/resources' },
    example: {
      heading: 'A customer portal with its database and a worker.',
      description: 'A booking app, a webhook receiver, an integration worker, or an early SaaS project.',
      steps: [
        { icon: 'rocket', heading: 'Deploy the app and its API', body: 'From code, Git, or a container image.' },
        { icon: 'database', heading: 'Connect its resources', body: 'Postgres, Valkey, volumes, and a background worker.' },
        { icon: 'history', heading: 'Ship the second release', body: 'Follow the logs, and roll back if it needs to be reversed.' },
      ],
    },
    capabilities: {
      heading: 'What a project can use',
      items: [
        { icon: 'server', heading: 'Web and API processes', body: 'With or without a browser interface.' },
        { icon: 'cpu', heading: 'Workers', body: 'Background processing as its own process.' },
        { icon: 'database', heading: 'PostgreSQL', body: 'For the data your app keeps.' },
        { icon: 'layers', heading: 'Valkey', body: 'For caching and queues.' },
        { icon: 'hard-drive', heading: 'Volumes', body: 'Persistent storage for files.' },
        { icon: 'scroll', heading: 'Logs and releases', body: 'See what runs, and every release that got there.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does my project need a web interface?', a: 'No. An API or a background worker is a project of its own.' },
        { q: 'Can I run a SaaS on it?', a: 'Yes, as an application. Subscriptions, tenant isolation inside your app, and customer authorization are your app\'s to provide.' },
        { q: 'Is there a scheduler for jobs?', a: 'Run your processing as a worker. Check the documentation for the workflows that are supported today.' },
      ],
    },
    related: [
      { label: 'Developers', href: '/solutions/developers' },
      { label: 'Apps & Databases Together', href: '/features/apps-and-databases' },
      { label: 'Project Management & Visibility', href: '/features/project-management' },
      { label: 'Client Projects', href: '/solutions/client-projects' },
    ],
    close: {
      heading: 'Try it with a project you are already developing.',
      description: 'Deploy it, connect what it needs, and publish the next version.',
      action: { label: 'Deploy a project', href: 'signup' },
    },
  },
  {
    slug: 'ai-apps-and-agents',
    name: 'AI Apps & Agents',
    card: 'Run applications and agents that use AI.',
    icon: 'cpu',
    label: 'AI Apps & Agents',
    heading: 'Deploy and run your AI apps and agents.',
    description: 'Run the application or agent beyond your machine, with its configuration, the data it keeps, and visibility into how it operates.',
    primary: { label: 'Try it with an AI project', href: 'signup' },
    secondary: { label: 'Read the concepts', href: '/docs/concepts' },
    example: {
      heading: 'A research agent, triggered through an API.',
      description: 'Or a document-processing app, or a background task that calls a model.',
      steps: [
        { icon: 'rocket', heading: 'Deploy the app or agent', body: 'As a web process, an API, or a worker.' },
        { icon: 'key', heading: 'Configure model access', body: 'Keep API keys and settings as secrets with the project.' },
        { icon: 'scroll', heading: 'Watch it work', body: 'Follow the logs as it runs, and its data where it keeps it.' },
      ],
    },
    capabilities: {
      heading: 'What it can use',
      items: [
        { icon: 'cpu', heading: 'Apps and workers', body: 'Run the agent as a web process or in the background.' },
        { icon: 'key', heading: 'Configuration and secrets', body: 'Model keys and settings, kept with the project.' },
        { icon: 'database', heading: 'Data resources', body: 'PostgreSQL, Valkey, and volumes for what it keeps.' },
        { icon: 'activity', heading: 'Deployment visibility', body: 'Logs, releases, and status as it runs.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does Shpyrd run the model?', a: 'No. Your application calls the external model service you choose; Shpyrd runs the application.' },
        { q: 'Who defines how the agent behaves?', a: 'Your framework and your code. Shpyrd gives it a place to run, its configuration, and visibility into its operation.' },
        { q: 'Can it run untrusted code from users?', a: 'Running arbitrary untrusted code is not part of what Shpyrd offers today.' },
      ],
    },
    related: [
      { label: 'Web Apps & APIs', href: '/solutions/web-apps' },
      { label: 'Apps & Databases Together', href: '/features/apps-and-databases' },
      { label: 'Project Management & Visibility', href: '/features/project-management' },
    ],
    close: {
      heading: 'Run it beyond your machine.',
      description: 'Deploy your AI app or agent with what it needs to keep running.',
      action: { label: 'Try it with an AI project', href: 'signup' },
    },
  },
  {
    slug: 'internal-apps',
    name: 'Internal Apps',
    card: 'Put internal tools into use with the right access.',
    icon: 'layout',
    label: 'Internal Apps',
    heading: 'Put your internal apps to work.',
    description: 'Make the tools your company relies on available to the right employees, with your company\'s login, and keep them easy to update.',
    primary: { label: 'Start with an internal app', href: 'signup' },
    secondary: { label: 'Read about app access', href: '/docs/app-access' },
    example: {
      heading: 'An approval tool, in use by the right team.',
      description: 'Or an operations dashboard, a calculator a team uses, a knowledge app.',
      steps: [
        { icon: 'key', heading: 'Sign in with the company login', body: 'People use the accounts they already have.' },
        { icon: 'layout', heading: 'Open it from the portal', body: 'Each person sees the apps they may use.' },
        { icon: 'refresh', heading: 'Update it', body: 'A new release reaches everyone who uses it.' },
      ],
    },
    capabilities: {
      heading: 'What makes it work',
      items: [
        { icon: 'lock', heading: 'Sign-in required', body: 'Restricted access for each application.' },
        { icon: 'users', heading: 'Workspace and project roles', body: 'Who can use, publish, and manage each app.' },
        { icon: 'key', heading: 'Company sign-in', body: 'Single sign-on with supported identity providers.' },
        { icon: 'layout', heading: 'An app launcher', body: 'One place to find the apps someone can open.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does it matter how the app was built?', a: 'No. Built with AI or conventionally, Shpyrd deploys it and controls who can open it.' },
        { q: 'Does Shpyrd set up the approval rules in my app?', a: 'No. Your app defines its process and its business permissions; Shpyrd controls access to the app.' },
        { q: 'Can we use our company login?', a: 'We can confirm SSO support for your identity provider and the offer that fits your setup.' },
      ],
    },
    related: [
      { label: 'Businesses', href: '/solutions/businesses' },
      { label: 'Built-in App Access', href: '/features/app-access' },
      { label: 'Automatic Sleep & Wake', href: '/features/automatic-sleep' },
    ],
    close: {
      heading: 'Which app would you like to put into use first?',
      description: 'Deploy it, give your team access, and keep it updated.',
      action: { label: 'Start with an internal app', href: 'signup' },
    },
  },
]

const clients: Landing[] = [
  {
    slug: 'client-projects',
    name: 'Client Projects',
    card: 'Deploy client projects and hand them over or keep maintaining them.',
    icon: 'briefcase',
    label: 'Client Projects',
    heading: 'Simplify how you deploy and manage your clients\' projects.',
    description: 'Get each client\'s project into use, then hand it over or keep maintaining it, with the access and environment the project needs.',
    primary: { label: 'Try it with a client project', href: 'signup' },
    secondary: { label: 'Managed White Label Infrastructure', href: '/solutions/white-label-infrastructure' },
    example: {
      heading: 'A client app, delivered and kept moving.',
      description: 'Deployed, updated, then handed over or maintained.',
      steps: [
        { icon: 'rocket', heading: 'Deploy the client\'s project', body: 'With the resources and access it needs.' },
        { icon: 'refresh', heading: 'Publish updates', body: 'Every change a release, with logs to diagnose problems.' },
        { icon: 'handshake', heading: 'Hand over or keep maintaining', body: 'The client can contract directly with Shpyrd, or you keep supporting the app.' },
      ],
    },
    capabilities: {
      heading: 'What helps the engagement',
      items: [
        { icon: 'rocket', heading: 'Deployment and releases', body: 'A repeatable workflow for every client project.' },
        { icon: 'users', heading: 'Access for both sides', body: 'Your team and your client, with their login where needed.' },
        { icon: 'scroll', heading: 'Visibility after launch', body: 'Logs, metrics, and releases to support the app.' },
      ],
    },
    questions: {
      heading: 'Questions partners ask',
      items: [
        { q: 'What does a handover include?', a: 'Agree on the handover, including access, accounts, contracts, credentials, documentation, and operating responsibilities.' },
        { q: 'Do I have to resell the infrastructure?', a: 'No. Your client can contract directly with Shpyrd, while you hand over or keep maintaining the app.' },
        { q: 'Can the project run in my client\'s cloud?', a: 'Shpyrd can run on AWS and Oracle Cloud. The selected offer defines who operates that environment.' },
      ],
    },
    related: [
      { label: 'FDEs & Implementation Partners', href: '/solutions/implementation-partners' },
      { label: 'Managed White Label Infrastructure', href: '/solutions/white-label-infrastructure' },
      { label: 'Built-in App Access', href: '/features/app-access' },
    ],
    close: {
      heading: 'Which client project would you like to start with?',
      description: 'Deploy it, keep it updated, and choose the path after delivery.',
      action: { label: 'Try it with a client project', href: 'signup' },
    },
  },
  {
    slug: 'white-label-infrastructure',
    name: 'Managed White Label Infrastructure',
    card: 'Offer managed infrastructure under your brand and set your prices.',
    icon: 'tag',
    label: 'Managed White Label Infrastructure',
    heading: 'Managed infrastructure. Your brand. Your prices.',
    description: 'Manage your client portfolio in one place, with one Shpyrd contract. Shpyrd handles the infrastructure and bills you.',
    primary: { label: 'Explore managed infrastructure for your clients', href: 'contact' },
    secondary: { label: 'Client Projects', href: '/solutions/client-projects' },
    example: {
      heading: 'Your client portfolio, under your brand.',
      description: 'How the responsibilities are shared.',
      steps: [
        { icon: 'layers', heading: 'Your portfolio in one place', body: 'Manage your clients\' projects together, under your brand.' },
        { icon: 'coins', heading: 'You set the prices', body: 'Choose what to charge your clients for the service.' },
        { icon: 'cloud', heading: 'Shpyrd runs the infrastructure', body: 'We operate it and bill you, under one contract.' },
      ],
    },
    capabilities: {
      heading: 'What the offer includes',
      items: [
        { icon: 'tag', heading: 'Your brand', body: 'The service your clients see is yours.' },
        { icon: 'file', heading: 'One contract', body: 'A single contract between you and Shpyrd.' },
        { icon: 'cloud', heading: 'Managed infrastructure', body: 'Shpyrd operates the infrastructure your clients\' apps run on.' },
      ],
    },
    questions: {
      heading: 'Practical questions',
      items: [
        { q: 'Does Shpyrd invoice my clients?', a: 'No. You set your prices and handle your clients\' billing; Shpyrd bills you.' },
        { q: 'Who supports the applications?', a: 'Shpyrd operates the infrastructure. Application work stays with you, as agreed with each client.' },
        { q: 'Is this the same as running in my client\'s cloud?', a: 'No. Self-hosting in a client\'s cloud and Shpyrd-managed infrastructure have different responsibilities.' },
      ],
    },
    related: [
      { label: 'Client Projects', href: '/solutions/client-projects' },
      { label: 'FDEs & Implementation Partners', href: '/solutions/implementation-partners' },
      { label: 'Fully Managed Cloud', href: '/features/fully-managed-cloud' },
    ],
    close: {
      heading: 'Offer managed infrastructure under your own brand.',
      description: 'Set your prices. Let Shpyrd handle the infrastructure.',
      action: { label: 'Explore the managed offering', href: 'contact' },
    },
  },
]

// The menu's three groups, in its order.
export const solutionGroups = [
  { name: 'Who', items: who },
  { name: 'Publish and Run', items: publish },
  { name: 'Deliver for Clients', items: clients },
]

export const solutions: Landing[] = [...who, ...publish, ...clients]
