// Packs what Next exported into one file per page, for the Go server to
// embed (pkg/pages): the stylesheet goes inline, without the fonts it
// would fetch; the scripts go, a page that says one thing needs none; the
// dark theme follows the system instead of a switch. Where the page has
// a Go template's words, they stay as they are.
// Run after `next build`: node scripts/pack.mjs
import { readFileSync, readdirSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";

const out = resolve("out");
const target = resolve("../../pkg/pages/html");
mkdirSync(target, { recursive: true });

const PAGES = ["nothing", "no-access", "waking", "mark"];

// The stylesheet the export wrote, as one text: the tokens of the dark
// theme under a media query, the fonts dropped, the rest as it is.
function stylesheet() {
  const dir = join(out, "_next/static");
  const files = [];
  const walk = (d) => {
    for (const f of readdirSync(d, { withFileTypes: true })) {
      if (f.isDirectory()) walk(join(d, f.name));
      else if (f.name.endsWith(".css")) files.push(join(d, f.name));
    }
  };
  walk(dir);
  let css = files.map((f) => readFileSync(f, "utf8")).join("\n");
  css = css.replace(/@font-face\{[^}]*\}/g, "");
  css = css.replace(/url\([^)]*\)/g, (m) => (m.includes("/_next/") ? "none" : m));
  // The dark tokens are written for `.dark`, the class the theme switch
  // sets; here the system says.
  css = css.replace(/([{}]|^)\.dark\{([^}]*)\}/g, (_, before, body) => `${before}@media (prefers-color-scheme:dark){:root{${body}}}`);
  return css;
}

const css = stylesheet();

function pack(name) {
  let html = readFileSync(join(out, `${name}.html`), "utf8");
  html = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, "");
  html = html.replace(/<link\b[^>]*>/g, "");
  html = html.replace(/<meta name="next-size-adjust"[^>]*>/g, "");
  html = html.replace(/<!--\$-->|<!--\/\$-->/g, "");
  html = html.replace(/<div hidden=""><\/div>/g, "");
  // The template unescaped: Next writes the words of the page as HTML,
  // and the Go template reads its own marks.
  html = html.replace(/\{\{[^}]*\}\}/g, (m) => m.replace(/&quot;/g, '"').replace(/&amp;/g, "&"));
  html = html.replace(
    "</head>",
    `<title>{{if .Title}}{{.Title}}{{else}}shpyrd{{end}}</title>{{if .Refresh}}<meta http-equiv="refresh" content="{{.Refresh}}">{{end}}<style>${css}</style></head>`,
  );
  writeFileSync(join(target, `${name}.html`), html);
  return html.length;
}

for (const name of PAGES) console.log(name, pack(name), "bytes");
