import path from "node:path";
import { readCopy } from "./copy.mjs";
import { parse } from "yaml";
import { valueToEstree } from "estree-util-value-to-estree";
import { slug } from "./heading-slug.mjs";

const blocks = {
  Hero: [], Steps: [], Capabilities: [], Questions: [], Related: [], Closing: [],
  Record: [], Action: ["destination"], Lead: [], Callout: ["title", "type"],
  Chat: ["title", "detail"], Message: ["from", "author"], MessageSteps: [],
  QuickLinks: [], QuickLink: ["title", "description", "icon", "href"],
  AgentSetup: [], AddToAgent: ["manual"],
};
const landingBlocks = ["Hero", "Steps", "Capabilities", "Questions", "Related", "Closing"];

function walk(node, visit) {
  visit(node);
  for (const child of node.children ?? []) walk(child, visit);
}

function expression(value) {
  return { type: "mdxJsxAttributeValueExpression", value: JSON.stringify(value), data: {
    estree: { type: "Program", sourceType: "module", body: [
      { type: "ExpressionStatement", expression: valueToEstree(value) },
    ] },
  } };
}

function element(name, props, children = []) {
  return { type: "mdxJsxFlowElement", name, attributes: Object.entries(props).map(([key, value]) => ({
    type: "mdxJsxAttribute", name: key, value: expression(value),
  })), children };
}

function exported(name, value) {
  return { type: "mdxjsEsm", value: "", data: {
    estree: { type: "Program", sourceType: "module", body: [{
      type: "ExportNamedDeclaration", specifiers: [], source: null,
      declaration: { type: "VariableDeclaration", kind: "const", declarations: [{
        type: "VariableDeclarator", id: { type: "Identifier", name }, init: valueToEstree(value),
      }] },
    }] },
  } };
}

const words = (node) => node.value ?? (node.children ?? []).map(words).join("");

/** Editorial MDX: validate authors' syntax before generating any JavaScript. */
export default function editorial() {
  return (tree, file) => {
    const fail = (message, node = tree) => file.fail(message, node);
    const fronts = tree.children.filter((node) => node.type === "yaml");
    if (fronts.length !== 1 || tree.children[0] !== fronts[0]) fail("Start the document with one YAML header (---). ");
    let meta;
    try { meta = parse(fronts[0].value); } catch (error) { fail(`Invalid YAML header: ${error.message}`, fronts[0]); }
    if (!meta || typeof meta !== "object" || Array.isArray(meta)) fail("The YAML header must contain named fields.");
    for (const key of meta.layout === "copy" ? ["title"] : ["title", "description"]) {
      if (typeof meta[key] !== "string" || !meta[key].trim()) fail(`The YAML header needs a non-empty ${key}.`, fronts[0]);
    }
    if (meta.layout !== undefined && !["landing", "copy"].includes(meta.layout)) fail('Use layout: landing or layout: copy.', fronts[0]);
    const isLanding = meta.layout === "landing";
    const allowed = meta.layout === "copy" ? ["Record"] : isLanding ? [...landingBlocks, "Action"] : ["Lead", "Callout", "AgentSetup", "AddToAgent", "Chat", "Message", "MessageSteps", "QuickLinks", "QuickLink"];
    const href = (url, node, action = false) => {
      if (action && ["signup", "contact"].includes(url)) return url;
      if (typeof url !== "string" || !/^(?:\/(?!\/)|#|https?:\/\/|mailto:)/.test(url)) {
        fail("Use a site path, anchor, https:// link, mailto: link, signup or contact.", node);
      }
      return url;
    };
    walk(tree, (node) => {
      if (["mdxjsEsm", "mdxFlowExpression", "mdxTextExpression"].includes(node.type)) {
        fail("Write Markdown and editorial blocks here; imports, exports and JavaScript expressions belong in the website.", node);
      }
      if (node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement") {
        if (!allowed.includes(node.name)) fail(`Unknown editorial block <${node.name ?? "fragment"}>. Available: ${allowed.join(", ")}.`, node);
        const used = new Set();
        for (const attr of node.attributes) {
          if (attr.type !== "mdxJsxAttribute" || !blocks[node.name].includes(attr.name) || used.has(attr.name)) {
            fail(`Unsupported or repeated attribute on <${node.name}>. Keep styling and behavior in the website.`, node);
          }
          used.add(attr.name);
          if (attr.value !== null && typeof attr.value !== "string") fail("Use quoted text for attributes, without JavaScript expressions.", node);
          if (attr.value === null && attr.name !== "manual") fail(`Give ${attr.name} a quoted value.`, node);
          if (attr.name === "type" && !["note", "warning"].includes(attr.value)) fail('Callout type must be "note" or "warning".', node);
          if (attr.name === "manual" && attr.value !== null) fail("Write <AddToAgent manual /> to show the manual link.", node);
        }
        if (["AgentSetup", "AddToAgent"].includes(node.name) && node.children.length) fail(`<${node.name} /> has no editable body.`, node);
      }
      if (meta.layout !== "copy" && ["link", "image", "definition"].includes(node.type)) href(node.url, node);
    });
    tree.children = tree.children.filter((node) => node.type !== "yaml");
    if (meta.layout === "copy") {
      const copy = readCopy(tree.children, file);
      tree.children = [exported("copy", copy), exported("frontmatter", meta)];
      return;
    }
    const headings = [];
    if (isLanding) {
      const menu = meta.menu;
      if (!menu || typeof menu.label !== "string" || !menu.label.trim() || typeof menu.description !== "string" || !menu.description.trim() || !Number.isInteger(menu.order)) {
        fail("A landing needs menu.label, menu.description and an integer menu.order in its header.", fronts[0]);
      }
      const section = (name) => {
        const found = tree.children.filter((node) => node.name === name);
        if (found.length !== 1) fail(`Use exactly one <${name}> block.`);
        return found[0];
      };
      if (tree.children.some((node) => !landingBlocks.includes(node.name))) fail("Put landing text inside Hero, Steps, Capabilities, Questions, Related or Closing.");
      // Compact inline Markdown, not React children: visual components receive named props.
      const inline = (nodes) => nodes.map((node) => {
        if (!["text", "strong", "emphasis", "delete", "inlineCode", "link", "break"].includes(node.type)) {
          fail("Use text, bold, italics, inline code or links in this paragraph.", node);
        }
        return { type: node.type, ...(node.value !== undefined && { value: node.value }),
          ...(node.url !== undefined && { url: node.url }), ...(node.children && { children: inline(node.children) }) };
      });
      const title = (node, depth) => {
        if (node?.type !== "heading" || node.depth !== depth) fail(`Start this part with ${"#".repeat(depth)} and its heading.`, node);
        if (node.children.some((child) => child.type !== "text")) fail("Use plain text in landing headings.", node);
        return words(node);
      };
      const paragraph = (node) => {
        if (node?.type !== "paragraph") fail("Write one paragraph here.", node);
        return inline(node.children);
      };
      const link = (node) => {
        if (node?.type !== "link") fail("Write a Markdown link: [label](/destination).", node);
        if (node.children.some((child) => child.type !== "text")) fail("Use plain text for navigation link labels.", node);
        return { label: words(node), href: href(node.url, node) };
      };
      const action = (node) => {
        if (node?.name !== "Action") fail('Write <Action destination="signup">Button label</Action>.', node);
        const destination = node.attributes.find((attr) => attr.name === "destination")?.value;
        if (!words(node).trim()) fail("Give the action a label.", node);
        const labelNodes = node.children.flatMap((child) => child.type === "paragraph" ? child.children : [child]);
        if (labelNodes.some((child) => child.type !== "text")) fail("Use plain text for button labels.", node);
        return { label: words(node).trim(), href: href(destination, node, true) };
      };
      const lead = section("Hero").children;
      if (lead.length < 3 || lead.length > 4) fail("Hero needs a # heading, one paragraph, an Action and optionally a secondary Markdown link.", section("Hero"));
      const hero = { heading: title(lead[0], 1), description: paragraph(lead[1]), primary: action(lead[2]) };
      if (lead[3]) {
        if (lead[3].type !== "paragraph" || lead[3].children.length !== 1) fail("The secondary action must be one Markdown link.", lead[3]);
        hero.secondary = link(lead[3].children[0]);
      }
      const items = (name) => {
        const children = [...section(name).children];
        const heading = title(children.shift(), 2);
        const description = children[0]?.type === "paragraph" ? paragraph(children.shift()) : undefined;
        const entries = [];
        while (children.length) entries.push({ heading: title(children.shift(), 3), body: paragraph(children.shift()) });
        if (!entries.length) fail(`<${name}> needs at least one ### heading and paragraph.`, section(name));
        return { heading, ...(description && { description }), items: entries };
      };
      const example = items("Steps");
      if (example.items.length !== 3) fail("The current landing template needs three steps.", section("Steps"));
      const capabilities = items("Capabilities");
      const questions = items("Questions");
      if (questions.description) fail("Put question answers under each ### heading, without a section introduction.", section("Questions"));
      const related = section("Related");
      if (related.children.length !== 1 || related.children[0].type !== "list") fail("Related needs a list of Markdown links.", related);
      const links = related.children[0].children.map((item) => {
        if (item.children.length !== 1 || item.children[0].type !== "paragraph" || item.children[0].children.length !== 1) fail("Use one Markdown link per list item.", item);
        return link(item.children[0].children[0]);
      });
      const close = section("Closing").children;
      if (close.length !== 3) fail("Closing needs a ## heading, one paragraph and an Action.", section("Closing"));
      tree.children = [element("EditorialLanding", { page: {
        slug: path.basename(file.path, ".mdx"), name: meta.title, label: menu.label, card: menu.description,
        ...hero, example, capabilities, questions, related: links,
        close: { heading: title(close[0], 2), description: paragraph(close[1]), action: action(close[2]) },
      } })];
    } else {
      const seen = new Map();
      const transform = (parent) => {
        parent.children = parent.children.map((node) => {
          if (node.type === "heading") {
            const title = words(node);
            const id = slug(title, seen);
            node.data = { ...node.data, hProperties: { id } };
            if ([2, 3].includes(node.depth)) headings.push({ id, title, level: node.depth });
          }
          if (node.name === "Lead") {
            if (node.children.length !== 1 || node.children[0].type !== "paragraph") fail("Lead needs one paragraph.", node);
            const paragraph = node.children[0];
            paragraph.data = { hProperties: { className: "lead" } };
            return paragraph;
          }
          if (node.name === "AddToAgent" && !node.attributes.some((attr) => attr.name === "manual")) {
            node.attributes.push({ type: "mdxJsxAttribute", name: "manual", value: expression(false) });
          }
          if (node.name === "Message") {
            const steps = node.children.filter((child) => child.name === "MessageSteps");
            if (steps.length > 1) fail("Use one MessageSteps block per Message.", node);
            if (steps.length) {
              const list = steps[0].children;
              if (list.length !== 1 || list[0].type !== "list") fail("MessageSteps needs a Markdown list.", steps[0]);
              node.attributes.push({ type: "mdxJsxAttribute", name: "steps", value: expression(list[0].children.map(words)) });
              node.children = node.children.filter((child) => child.name !== "MessageSteps");
            }
          }
          if (node.name === "MessageSteps") fail("Put MessageSteps inside Message.", node);
          if (node.type === "code") return element("Fence", { content: node.value, language: node.lang ?? "" });
          if (node.type === "image") node.data = { ...node.data, hProperties: { loading: "lazy" } };
          if (node.children) transform(node);
          return node;
        });
      };
      transform(tree);
    }
    tree.children.unshift(exported("frontmatter", meta), exported("headings", headings));
  };
}
