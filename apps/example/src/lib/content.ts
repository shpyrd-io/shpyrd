import fs from "node:fs";
import path from "node:path";
import Markdoc, { Tag, type Config, type RenderableTreeNode } from "@markdoc/markdoc";

// The texts of the site, read where they are today and as they are: the
// Markdoc files of content/. Nothing is copied. CONTENT_DIR names another
// folder, relative to this application.

const dir = path.resolve(process.cwd(), process.env.CONTENT_DIR ?? "../../content");

// The pictures of the texts are served by the site itself.
const site = "https://shpyrd.io";

export type Heading = { id: string; title: string; level: number };

export type Text = {
  title: string;
  description?: string;
  content: RenderableTreeNode;
  headings: Heading[];
};

/** The words of a node, without what draws them. */
function words(node: RenderableTreeNode | RenderableTreeNode[]): string {
  if (Array.isArray(node)) return node.map(words).join("");
  if (typeof node === "string") return node;
  if (Tag.isTag(node)) return words(node.children);
  return "";
}

/** A name for an address, made of the words of a heading. */
export function slug(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

/** The front matter of these files is `key: value` lines, nothing more. */
export function meta(front: string | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of (front ?? "").split("\n")) {
    const m = /^(\w+):\s*(.*)$/.exec(line);
    if (m) out[m[1]] = m[2].replace(/^['"]|['"]$/g, "");
  }
  return out;
}

// The tags and nodes of the texts, each drawn by a component of design/ui
// (app/markdoc.tsx says which).
const config: Config = {
  tags: {
    // A conversation with an agent, drawn as the agent's own window: a
    // `chat`, holding a `message` for each turn. An agent's turn may list
    // what it did before it answered, in `steps`.
    // How to add shpyrd to each agent: a tab for each.
    "agent-setup": { render: "AgentSetup", selfClosing: true },
    chat: {
      render: "Chat",
      attributes: { title: { type: String, default: "Your agent" }, detail: { type: String } },
    },
    message: {
      render: "Message",
      attributes: {
        from: { type: String, default: "person", matches: ["person", "agent"] },
        author: { type: String },
        steps: { type: Array },
      },
    },
    callout: {
      render: "Callout",
      attributes: {
        title: { type: String },
        type: { type: String, default: "note", matches: ["note", "warning"] },
      },
    },
    "quick-links": { render: "QuickLinks" },
    "quick-link": {
      render: "QuickLink",
      selfClosing: true,
      attributes: {
        title: { type: String },
        description: { type: String },
        icon: { type: String },
        href: { type: String },
      },
    },
  },
  nodes: {
    fence: {
      render: "Fence",
      attributes: { language: { type: String }, content: { type: String } },
    },
    heading: {
      children: ["inline"],
      attributes: { level: { type: Number, required: true, default: 1 } },
      transform(node, cfg) {
        const children = node.transformChildren(cfg);
        return new Tag(
          `h${node.attributes.level}`,
          { ...node.transformAttributes(cfg), id: slug(words(children)) },
          children,
        );
      },
    },
    image: {
      attributes: { src: { type: String }, alt: { type: String }, title: { type: String } },
      transform(node) {
        const { src, alt, title } = node.attributes;
        return new Tag("img", {
          src: String(src).startsWith("/") ? site + src : src,
          alt,
          title,
          loading: "lazy",
        });
      },
    },
  },
};

function headings(
  node: RenderableTreeNode | RenderableTreeNode[],
  out: Heading[] = [],
): Heading[] {
  if (Array.isArray(node)) {
    for (const n of node) headings(n, out);
    return out;
  }
  if (!Tag.isTag(node)) return out;
  const m = /^h([23])$/.exec(node.name);
  if (m) out.push({ id: String(node.attributes.id), title: words(node.children), level: Number(m[1]) });
  else headings(node.children, out);
  return out;
}

/** A text by the name of its file: "docs/getting-started", or "docs/installation". */
export function read(name: string): Text {
  const ast = Markdoc.parse(fs.readFileSync(path.join(dir, `${name}.md`), "utf8"));
  const m = meta(ast.attributes.frontmatter);
  const content = Markdoc.transform(ast, config);
  return {
    title: m.title ?? name,
    description: m.description,
    content,
    headings: headings(content),
  };
}

/** The names of the documents, as their addresses have them. */
export function documents(): string[] {
  return fs
    .readdirSync(path.join(dir, "docs"))
    .filter((f) => f.endsWith(".md"))
    .map((f) => f.replace(/\.md$/, ""))
    .sort();
}
