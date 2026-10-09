// How each feature and solution page is drawn (Giovani, 2026-10-09: the
// pages looked too alike). The words stay in content/site; this only picks,
// for each page, the blocks of the site and of design/ui that show its message
// best. landing.tsx draws them.

export type Design = {
  // The top: centred words, or the words beside a picture of the product.
  hero: "center" | "terminal" | "yaml" | "worker" | "launcher" | "signin" | "browser";
  // Behind the top: the binary ship, or the turning circle of containers.
  background: "mark" | "circle";
  // The example: a loop (steps that come round again), the site's vertical
  // route, three steps side by side, three options side by side (not steps),
  // or the steps told as a conversation with an agent.
  example: "loop" | "route" | "columns" | "options" | "chat";
  // The capabilities: glass cards, an open grid, or a checklist beside a
  // picture.
  capabilities: "cards" | "grid" | "checklist";
  // The picture beside the checklist.
  picture?: "terminal" | "yaml" | "worker" | "launcher" | "signin" | "browser";
  // The questions: to open one at a time, or all read side by side.
  questions: "faq" | "columns";
  // The end: the simple CTA, or the large CTA block with the shipyard.
  close: "simple" | "shipyard";
};

export const designs: Record<string, Design> = {
  // Features
  // Deploying is a command: its output beside the words; the loop of deploy,
  // open, deploy again.
  "easy-deployment": { hero: "terminal", background: "mark", example: "loop", capabilities: "grid", questions: "columns", close: "simple" },
  // Infrastructure someone else runs: the turning containers behind, and the
  // shipyard at the end.
  "fully-managed-cloud": { hero: "center", background: "circle", example: "columns", capabilities: "cards", questions: "faq", close: "shipyard" },
  // An app and what it needs, as the file that declares them.
  "apps-and-databases": { hero: "yaml", background: "mark", example: "route", capabilities: "cards", questions: "columns", close: "simple" },
  // Access: the sign-in screen people meet, then the launcher of their apps.
  "app-access": { hero: "signin", background: "mark", example: "route", capabilities: "checklist", picture: "launcher", questions: "faq", close: "simple" },
  "automatic-sleep": { hero: "center", background: "mark", example: "loop", capabilities: "grid", questions: "columns", close: "simple" },
  "project-management": { hero: "terminal", background: "mark", example: "loop", capabilities: "cards", questions: "faq", close: "simple" },

  // Solutions · Who
  // Made with an agent: the agent tells it.
  "ai-community": { hero: "center", background: "circle", example: "chat", capabilities: "grid", questions: "faq", close: "shipyard" },
  // A site going on the internet: a browser.
  "vibe-coders": { hero: "browser", background: "mark", example: "loop", capabilities: "cards", questions: "columns", close: "simple" },
  "developers": { hero: "terminal", background: "mark", example: "route", capabilities: "checklist", picture: "yaml", questions: "faq", close: "shipyard" },
  // The apps a company's people open.
  "businesses": { hero: "launcher", background: "mark", example: "columns", capabilities: "cards", questions: "faq", close: "simple" },
  // Three ways to work with a client: options, not steps.
  "implementation-partners": { hero: "center", background: "mark", example: "options", capabilities: "grid", questions: "columns", close: "simple" },

  // Solutions · Publish and Run
  "apps-built-with-ai": { hero: "center", background: "circle", example: "chat", capabilities: "cards", questions: "faq", close: "shipyard" },
  "websites": { hero: "browser", background: "mark", example: "columns", capabilities: "grid", questions: "columns", close: "simple" },
  "web-apps": { hero: "yaml", background: "mark", example: "route", capabilities: "cards", questions: "faq", close: "simple" },
  // An agent with no web page: the worker that runs it.
  "ai-apps-and-agents": { hero: "worker", background: "mark", example: "columns", capabilities: "checklist", picture: "terminal", questions: "columns", close: "simple" },
  "internal-apps": { hero: "launcher", background: "mark", example: "route", capabilities: "cards", questions: "faq", close: "simple" },

  // Solutions · Deliver for Clients
  "client-projects": { hero: "signin", background: "mark", example: "route", capabilities: "grid", questions: "columns", close: "simple" },
  "white-label-infrastructure": { hero: "center", background: "circle", example: "options", capabilities: "cards", questions: "faq", close: "shipyard" },
};

// A page not listed above is drawn as they all were at first.
export const plain: Design = { hero: "center", background: "mark", example: "route", capabilities: "cards", questions: "faq", close: "simple" };
