import type { Landing } from "@/content/landings";

// The review's words for Easy Deployment (2026-11-12), for its proposal.
export const page: Landing = {
  slug: "easy-deployment",
  name: "Easy Deployment",
  card: "Your app, online in one command.",
  icon: "rocket",
  label: "Easy Deployment",
  heading: "Your app, online in one command.",
  description:
    "Your AI built it. One command, or one sentence to your agent, puts it online at its own address, open to everyone or behind your sign-in. Every change after that goes out the same way.",
  primary: { label: "Deploy your app for free", href: "signup" },
  secondary: { label: "Read the deployment guide", href: "/docs/deploying" },
  example: {
    heading: "Ship it. Share it. Change it.",
    description: "From your laptop to a link your team opens, and every change after.",
    steps: [
      { icon: "rocket", heading: "Put it online", body: "From the folder your AI worked in, or a GitHub repository. We build it for you." },
      { icon: "globe", heading: "Share the link", body: "It gets its own address, or your domain, such as app.yourapp.com." },
      {
        icon: "refresh",
        heading: "Change it anytime",
        body: "Deploy again and the new version goes live. Something broke? Go back to the last one in one step.",
      },
    ],
  },
  capabilities: {
    heading: "Why it just works",
    items: [
      { icon: "bot", heading: "Ask your agent", body: 'Claude Code, Cursor or Codex can deploy for you. Tell it: "Publish this app."' },
      { icon: "package", heading: "No server to set up", body: "We detect what your app is built with and build it. No Docker needed." },
      { icon: "code", heading: "From anywhere", body: "A folder, a GitHub repository, or a container image if you already have one." },
      { icon: "history", heading: "Every version saved", body: "Each deploy is a release you can look back at and return to." },
      { icon: "globe", heading: "Your own address", body: "Every app gets one; bring yours, such as yourapp.com." },
      { icon: "terminal", heading: "For developers too", body: "The same deploy from the CLI and the API, for scripts and CI." },
    ],
  },
  questions: {
    heading: "Practical questions",
    items: [
      {
        q: "Do I need Docker or a config file?",
        a: "Usually not: we detect most apps and build them. If yours needs settings, the guide shows how.",
      },
      { q: "Can my AI agent deploy for me?", a: "Yes. Once you sign in, your agent can publish and update the app for you." },
      {
        q: "What if a new version breaks something?",
        a: "Go back to the previous version in one step, from the CLI or by asking your agent.",
      },
      {
        q: "Can I use my own domain?",
        a: "Yes. Every app gets an address, and you can point your domain, such as app.yourapp.com, to it.",
      },
    ],
  },
  related: [
    { label: "Apps Built with AI", href: "/solutions/apps-built-with-ai" },
    { label: "Websites & Landing Pages", href: "/solutions/websites" },
    { label: "Web Apps & APIs", href: "/solutions/web-apps" },
    { label: "Client Projects", href: "/solutions/client-projects" },
  ],
  close: {
    heading: "Which app should we put online?",
    description: "It's free to start. Deploy it today, and every change after.",
    action: { label: "Deploy your app for free", href: "signup" },
  },
};
