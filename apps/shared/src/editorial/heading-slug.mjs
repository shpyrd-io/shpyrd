/**
 * Preserve the anchors already published by the Markdoc reader.
 * @param {string} text
 * @param {Map<string, number>} [seen]
 */
export function slug(text, seen) {
  const base = text.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "section";
  if (!seen) return base;
  let n = seen.get(base) ?? 0;
  let name = base;
  while (seen.has(name)) name = `${base}-${++n + 1}`;
  seen.set(base, n);
  seen.set(name, n);
  return name;
}
