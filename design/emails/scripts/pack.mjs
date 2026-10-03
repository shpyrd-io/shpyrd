// Packs what Next exported into one Go template per email, for the server
// to embed (pkg/emails): the email itself, taken out of the page Next
// wrote around it, in a document of its own; and the subjects, in one
// file beside them. Where an email has a Go template's words, they stay
// as they are.
// Run after `next build`: node scripts/pack.mjs
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";

const out = resolve("out");
const target = resolve("../../pkg/emails/html");
mkdirSync(target, { recursive: true });

// The subjects and the names, from the list the gallery shows. Next wrote
// it into each page as data; reading the source is plainer.
const source = readFileSync(resolve("src/emails.tsx"), "utf8");
const names = [...source.matchAll(/^  "?([a-z-]+)"?: \{\n    title:/gm)].map((m) => m[1]);
const subjects = Object.fromEntries(
  names.map((name) => {
    const at = source.indexOf(`\n  ${name.includes("-") ? `"${name}"` : name}: {`);
    const subject = source.slice(at).match(/subject: "([^"]*)"/)[1];
    return [name, subject];
  }),
);

function pack(name) {
  const page = readFileSync(join(out, "template", `${name}.html`), "utf8");
  const start = page.indexOf('<div id="email"');
  const end = page.lastIndexOf("</body>");
  if (start < 0 || end < 0) throw new Error(`${name}: no email in the page`);
  let email = page.slice(start, end);
  // What Next writes after the page: its scripts and their data.
  email = email.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, "");
  email = email.replace(/<!--\$-->|<!--\/\$-->|<!-- -->/g, "");
  email = email.replace(/<div hidden=""><\/div>/g, "");
  email = email.replace(/<next-route-announcer[\s\S]*?<\/next-route-announcer>/g, "");
  // The template unescaped: Next writes the words as HTML, and the Go
  // template reads its own marks.
  email = email.replace(/\{\{[^}]*\}\}/g, (m) => m.replace(/&quot;/g, '"').replace(/&amp;/g, "&"));
  const html =
    `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
    `<meta name="viewport" content="width=device-width,initial-scale=1">` +
    `<meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light">` +
    `<title>${subjects[name]}</title></head>` +
    `<body style="margin:0;padding:0;background:#ffffff">${email.trim()}</body></html>\n`;
  writeFileSync(join(target, `${name}.html`), html);
  return html.length;
}

for (const name of names) console.log(name, pack(name), "bytes");
writeFileSync(join(target, "subjects.json"), JSON.stringify(subjects, null, 2) + "\n");
