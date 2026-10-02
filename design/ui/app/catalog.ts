// What the gallery shows, and where: the categories, and the pages of each.

export type Page = { slug: string; title: string; description: string };

export type Category = {
  slug: string;
  title: string;
  description: string;
  pages: Page[];
};

export const catalog: Category[] = [
  {
    slug: "foundations",
    title: "Foundations",
    description: "The smallest building blocks: colours, typography, icons.",
    pages: [
      { slug: "brand", title: "Brand", description: "The mark and the wordmark." },
      { slug: "colours", title: "Colours", description: "The colours by name, in both themes." },
      { slug: "typography", title: "Typography", description: "The fonts and the sizes of text." },
      { slug: "motion", title: "Motion", description: "How long a change takes, and how it speeds up and slows down." },
      { slug: "icons", title: "Icons", description: "Where the icons come from, the marks of the AI tools, and how big they are." },
      { slug: "app-icons", title: "App icons", description: "The symbols a project may take on its card, in the colour it chooses, or one of its own." },
      { slug: "button", title: "Button", description: "Something to press, with or without an icon." },
      { slug: "avatar", title: "Avatar", description: "The picture of someone, or of something that is not a person." },
      { slug: "badge", title: "Badge", description: "A word that marks something." },
      { slug: "counter-label", title: "Counter label", description: "How many there are of something, after its name." },
      { slug: "status-badge", title: "Status badge", description: "How something is: a dot, a word, a quantity." },
      { slug: "keybinding-hint", title: "Keybinding hint", description: "The keys of a shortcut, drawn." },
      { slug: "truncate", title: "Truncate", description: "Text that does not fit, cut with an ellipsis." },
      { slug: "tooltip", title: "Tooltip", description: "A few words over what the pointer is on." },
      { slug: "skeleton", title: "Skeleton", description: "The shape of what is coming, while it comes." },
      { slug: "inline-code", title: "Inline code", description: "A short piece of code in the middle of a text." },
    ],
  },
  {
    slug: "structures",
    title: "Structures",
    description: "Reusable combinations, like forms and cards.",
    pages: [
      { slug: "input", title: "Input", description: "Where something is typed, with what goes beside its text." },
      { slug: "textarea", title: "Textarea", description: "Where more than a line is typed." },
      { slug: "field", title: "Field", description: "A label and its field, with the hint and the error." },
      { slug: "color-picker", title: "Colour picker", description: "A colour, chosen: a swatch, the hex, and the colours the application offers." },
      { slug: "file-drop", title: "File drop", description: "A file dropped or chosen; what is there, shown, changed or removed." },
      { slug: "icon-picker", title: "Icon picker", description: "The icon of a card, chosen: a symbol and its colour, or an image of its own." },
      { slug: "switch", title: "Switch", description: "A setting that is on or off, and takes effect at once." },
      { slug: "card", title: "Card", description: "A box for what belongs together." },
      { slug: "avatar-stack", title: "Avatar stack", description: "Who is in it: pictures over each other, spread under the pointer." },
      { slug: "toast", title: "Toast", description: "A word in the corner, for a moment: what was just done, or what went wrong." },
      { slug: "alert", title: "Alert", description: "Something the person has to know: what goes on, went well, asks for care or went wrong." },
      { slug: "table", title: "Table", description: "Rows and columns." },
      { slug: "info-table", title: "Info table", description: "What is known of something, as names and their values." },
      { slug: "blankslate", title: "Blankslate", description: "What is shown where there is nothing yet." },
      { slug: "timeline", title: "Timeline", description: "What happened, in the order it happened." },
      { slug: "meter", title: "Meter", description: "How much is taken, of how much there is." },
      { slug: "progress-bar", title: "Progress bar", description: "How far something has gone, or of what parts it is made." },
      { slug: "stat", title: "Stat", description: "A number that matters, and how it went lately." },
      { slug: "pillar", title: "Pillar", description: "One of the things a section says, beside its siblings." },
      { slug: "section-intro", title: "Section intro", description: "What opens a section: what it is about, and why." },
      { slug: "app-window", title: "App window", description: "A desktop app as it sits on a screen: its title bar, what it shows, what is at its foot." },
      { slug: "browser-frame", title: "Browser frame", description: "A page as someone sees it: the window, its address, what is there." },
      { slug: "conversation", title: "Conversation", description: "A person and an agent taking turns, with what the agent did." },
      { slug: "prose", title: "Prose", description: "A text that was written: headings, paragraphs, lists, tables." },
    ],
  },
  {
    slug: "blueprints",
    title: "Blueprints",
    description: "Complex interactive components, like navigation headers.",
    pages: [
      { slug: "dropdown-button", title: "Dropdown button", description: "A button that opens a menu." },
      { slug: "tabs", title: "Tabs", description: "Views that take turns in the same place." },
      { slug: "dialog", title: "Dialog", description: "A question over the page, with its actions." },
      { slug: "anchored-overlay", title: "Anchored overlay", description: "A box that floats beside what opens it." },
      { slug: "minimal-footer", title: "Minimal footer", description: "The foot of a site: its links, where else to find you, the mark and one line." },
      { slug: "segmented-nav", title: "Segmented nav", description: "The pages of a section side by side, the open one lit by a light that slides to it." },
      { slug: "site-header", title: "Site header", description: "The bar over every page: the brand, the pages, and the way in." },
      { slug: "navigation-menu", title: "Navigation menu", description: "Words in a bar that open a panel of pages, each with a line on what it is." },
      { slug: "nav-list", title: "Nav list", description: "The links of a navigation, one under the other." },
      { slug: "breadcrumbs", title: "Breadcrumbs", description: "Where the page is, among the pages over it." },
      { slug: "time-chart", title: "Time chart", description: "How something changed along the time." },
      { slug: "event-strip", title: "Event strip", description: "What happened and when, a row for each kind." },
      { slug: "launcher-card", title: "Launcher card", description: "An application as the people who open it see it: what it is, how it is, where it answers." },
      { slug: "log-view", title: "Log view", description: "The lines of a log as they come: when, from where, what, and the fields of a record." },
      { slug: "shell-view", title: "Shell view", description: "A shell into a running instance, in a terminal that follows the theme." },
      { slug: "shipyard", title: "Shipyard", description: "A shipyard at work, for the first page: ships, a crane, trucks and containers, coming and going." },
      { slug: "ide", title: "IDE", description: "Code to be read: files in tabs, the numbers of the lines, a copy." },
    ],
  },
  {
    slug: "layouts",
    title: "Layouts & Pages",
    description: "Page-level placement and final views.",
    pages: [
      { slug: "hero", title: "Hero", description: "The banner at the top of a page: what it is, and what to do about it." },
      { slug: "page-layout", title: "Page layout", description: "The areas of a page: header, content, pane, sidebar, footer." },
      { slug: "page-heading", title: "Page heading", description: "The title of a page, what explains it and what can be done." },
      { slug: "stack", title: "Stack", description: "Things one after the other, with the same gap between them." },
      { slug: "metrics", title: "Metrics of a project", description: "A view made of the charts and the rest of the library." },
      { slug: "launcher", title: "Application launcher", description: "Where everyone lands: the applications of the workspace, centred, the settings in the corner." },
      { slug: "workspace-metrics", title: "Metrics of a workspace", description: "All the projects of a workspace together, and what the plan allows." },
      { slug: "cluster", title: "Cluster", description: "The machines under everything: what is used, what is reserved, how many there are." },
      { slug: "state", title: "State pages", description: "A whole page that says one thing: nothing here yet, no access, waking up, or the mark alone." },
      { slug: "application", title: "Application", description: "A whole application as the console and the workspace draw one: the frame, a list, one thing in its tabs, the settings." },
    ],
  },
];

export function href(category: Category, page: Page) {
  return `/${category.slug}/${page.slug}`;
}

// The category and the page an address belongs to.
export function find(path: string) {
  for (const category of catalog) {
    for (const page of category.pages) {
      if (href(category, page) === path.replace(/\/$/, "")) return { category, page };
    }
  }
  return undefined;
}
