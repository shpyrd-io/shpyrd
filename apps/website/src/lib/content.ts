import fs from "node:fs";
import path from "node:path";
import Markdoc, { Tag, type Config, type RenderableTreeNode } from "@markdoc/markdoc";

// The texts of the site, read where they live: the Markdoc files of
// content/docs. CONTENT_DIR names another folder, relative to this
// application.

const dir = path.resolve(process.cwd(), process.env.CONTENT_DIR ?? "../../content/docs");

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

/**
 * A name for an address, made of the words of a heading. Two headings of the
 * same words in one document get different names: the second is "-2".
 */
export function slug(text: string, seen?: Map<string, number>): string {
  const base =
    text
      .toLowerCase()
      .normalize("NFD")
      .replace(/[̀-ͯ]/g, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "section";
  if (!seen) return base;
  // Every name given out is recorded, not just the base: a document may have
  // both "Status" twice and a heading of its own called "Status 2".
  let n = seen.get(base) ?? 0;
  let name = base;
  while (seen.has(name)) name = `${base}-${++n + 1}`;
  seen.set(base, n);
  seen.set(name, n);
  return name;
}

/**
 * The front matter of these files is `key: value` lines, and the folded and
 * literal scalars YAML writes as `key: >` or `key: |` with the text indented
 * under them. A folded scalar joins its lines with a space.
 */
export function meta(front: string | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  const lines = (front ?? "").split("\n");

  for (let i = 0; i < lines.length; i++) {
    const m = /^(\w+):\s*(.*)$/.exec(lines[i]);
    if (!m) continue;

    const [, key, rest] = m;
    if (rest !== ">" && rest !== "|") {
      out[key] = rest.replace(/^['"]|['"]$/g, "");
      continue;
    }

    const block: string[] = [];
    while (i + 1 < lines.length && /^\s+\S/.test(lines[i + 1])) block.push(lines[++i].trim());
    out[key] = block.join(rest === ">" ? " " : "\n");
  }

  return out;
}

// The tags and nodes of the texts, each drawn by a component of design/ui
// (app/markdoc.tsx says which). Built per document, because the names of the
// headings are counted within one.
function configFor(seen: Map<string, number>): Config {
  return {
    tags: {
      // A conversation with an agent, drawn as the agent's own window: a
      // `chat`, holding a `message` for each turn. An agent's turn may list
      // what it did before it answered, in `steps`.
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
            { ...node.transformAttributes(cfg), id: slug(words(children), seen) },
            children,
          );
        },
      },
      image: {
        attributes: { src: { type: String }, alt: { type: String }, title: { type: String } },
        transform(node) {
          const { src, alt, title } = node.attributes;
          // The pictures are in public/, served by this site: an absolute
          // address would send a preview and a local build to production.
          return new Tag("img", { src, alt, title, loading: "lazy" });
        },
      },
    },
  };
}

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

/** A text by the name of its file: "getting-started". */
export function read(name: string): Text {
  const ast = Markdoc.parse(fs.readFileSync(path.join(dir, `${name}.md`), "utf8"));
  const m = meta(ast.attributes.frontmatter);
  const content = Markdoc.transform(ast, configFor(new Map()));
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
    .readdirSync(dir)
    .filter((f) => f.endsWith(".md"))
    .map((f) => f.replace(/\.md$/, ""))
    .sort();
}
