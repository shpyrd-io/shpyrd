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
      { slug: "icons", title: "Icons", description: "Where the icons come from and how big they are." },
      { slug: "button", title: "Button", description: "Something to press, with or without an icon." },
      { slug: "badge", title: "Badge", description: "A word that marks something." },
      { slug: "status-badge", title: "Status badge", description: "How something is: a dot, a word, a quantity." },
      { slug: "keybinding-hint", title: "Keybinding hint", description: "The keys of a shortcut, drawn." },
      { slug: "truncate", title: "Truncate", description: "Text that does not fit, cut with an ellipsis." },
      { slug: "inline-code", title: "Inline code", description: "A short piece of code in the middle of a text." },
    ],
  },
  {
    slug: "structures",
    title: "Structures",
    description: "Reusable combinations, like forms and cards.",
    pages: [
      { slug: "input", title: "Input", description: "Where something is typed, with what goes beside its text." },
      { slug: "field", title: "Field", description: "A label and its field, with the hint and the error." },
      { slug: "card", title: "Card", description: "A box for what belongs together." },
      { slug: "alert", title: "Alert", description: "Something the person has to know." },
      { slug: "table", title: "Table", description: "Rows and columns." },
      { slug: "info-table", title: "Info table", description: "What is known of something, as names and their values." },
      { slug: "blankslate", title: "Blankslate", description: "What is shown where there is nothing yet." },
      { slug: "timeline", title: "Timeline", description: "What happened, in the order it happened." },
      { slug: "meter", title: "Meter", description: "How much is taken, of how much there is." },
      { slug: "stat", title: "Stat", description: "A number that matters, and how it went lately." },
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
      { slug: "nav-list", title: "Nav list", description: "The links of a navigation, one under the other." },
      { slug: "breadcrumbs", title: "Breadcrumbs", description: "Where the page is, among the pages over it." },
      { slug: "time-chart", title: "Time chart", description: "How something changed along the time." },
      { slug: "event-strip", title: "Event strip", description: "What happened and when, a row for each kind." },
      { slug: "ide", title: "IDE", description: "Code to be read: files in tabs, the numbers of the lines, a copy." },
    ],
  },
  {
    slug: "layouts",
    title: "Layouts & Pages",
    description: "Page-level placement and final views.",
    pages: [
      { slug: "page-layout", title: "Page layout", description: "The areas of a page: header, content, pane, sidebar, footer." },
      { slug: "page-heading", title: "Page heading", description: "The title of a page, what explains it and what can be done." },
      { slug: "stack", title: "Stack", description: "Things one after the other, with the same gap between them." },
      { slug: "metrics", title: "Metrics of a project", description: "A view made of the charts and the rest of the library." },
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
