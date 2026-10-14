// Headings name fields; paragraphs, lists and tables hold their values.
// Parsed at compilation time, never by a browser or a React component.
export function fieldName(label) {
  const words = label.trim().split(/\s+/);
  return words.map((word, index) => index ? word[0].toUpperCase() + word.slice(1) : word[0].toLowerCase() + word.slice(1)).join("");
}

export function readCopy(nodes, file) {
  const fail = (message, node) => file.fail(message, node);
  const text = (node) => node.value ?? (node.children ?? []).map(text).join("");
  const scalar = (node) => {
    const value = text(node);
    if (node.type === "code" || node.children?.some((child) => child.type === "inlineCode")) return value;
    if (/^-?(?:0|[1-9]\d*)(?:\.\d+)?$/.test(value)) return Number(value);
    if (value === "null") return null;
    if (value === "true" || value === "false") return value === "true";
    return value;
  };
  const value = (children) => {
    if (!children.length) return "";
    if (children[0].type === "heading") return record(children);
    if (children.every((node) => node.name === "Record")) return children.map((node) => record(node.children));
    if (children.length !== 1) fail("Use a paragraph, a list, a table, a code block or named subsections for this field.", children[1]);
    const node = children[0];
    if (node.type === "paragraph" || node.type === "code") return scalar(node);
    if (node.type === "list") return node.children.map((item) => value(item.children));
    if (node.type === "table") {
      const [header, ...rows] = node.children;
      const keys = header.children.map((cell) => fieldName(text(cell)));
      if (new Set(keys).size !== keys.length) fail("Table column names must be unique.", header);
      return rows.map((row) => Object.fromEntries(row.children.flatMap((cell, index) => {
        const cellValue = scalar(cell);
        // An empty cell means this optional field is absent; ` ` is not used as a value.
        return cellValue === "" ? [] : [[keys[index], cellValue]];
      })));
    }
    fail("This field needs plain text, a list, a table, a code block or subsections.", node);
  };
  const record = (children) => {
    if (!children.length || children[0].type !== "heading") fail("Start a content record with a heading naming its first field.", children[0]);
    const level = children[0].depth;
    const result = {};
    let index = 0;
    while (index < children.length) {
      const heading = children[index++];
      if (heading.type !== "heading" || heading.depth !== level) fail("Keep sibling field headings at the same level.", heading);
      const key = fieldName(text(heading));
      if (["__proto__", "constructor", "prototype"].includes(key) || Object.hasOwn(result, key)) fail(`Repeated or reserved field: ${key}.`, heading);
      const start = index;
      while (index < children.length && !(children[index].type === "heading" && children[index].depth <= level)) index++;
      result[key] = value(children.slice(start, index));
    }
    return result;
  };
  return record(nodes);
}
