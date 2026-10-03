// Packs what Next exported into one file per page, for the Go server to
// embed (pkg/pages): the stylesheet goes inline, without the fonts it
// would fetch; the scripts go, a page that says one thing needs none; the
// dark theme follows the system instead of a switch. Where the page has
// a Go template's words, they stay as they are.
//
// A page that moves (the shipyard of the 503) has a script, the one
// thing it may run: bundled here with the files of its pieces in it, and
// written beside the page. The server writes it at the end of the page
// and names its hash in the page's policy (pkg/pages), so the page needs
// nothing from the host that is not answering.
// Run after `next build`: node scripts/pack.mjs
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { build } from "rolldown";

const out = resolve("out");
const target = resolve("../../pkg/pages/html");
mkdirSync(target, { recursive: true });

const PAGES = ["nothing", "no-access", "waking", "mark", "service-unavailable", "consent"];

// The pages with a script, and where it comes from.
const SCRIPTS = { "service-unavailable": "scripts/shipyard.ts" };

// The stylesheet of the pages, as one text: the one the pages link to
// (the gallery's is another), the tokens of the dark theme under a media
// query, the fonts dropped, the rest as it is.
function stylesheet() {
  const page = readFileSync(join(out, "template", `${PAGES[0]}.html`), "utf8");
  const hrefs = [...page.matchAll(/<link rel="stylesheet" href="([^"]+\.css)"/g)].map((m) => m[1]);
  if (hrefs.length === 0) throw new Error("the pages link to no stylesheet");
  let css = hrefs.map((href) => readFileSync(join(out, href), "utf8")).join("\n");
  css = css.replace(/@font-face\{[^}]*\}/g, "");
  css = css.replace(/url\([^)]*\)/g, (m) => (m.includes("/_next/") ? "none" : m));
  // The dark tokens are written for `.dark`, the class the theme switch
  // sets; here the system says.
  css = css.replace(/([{}]|^)\.dark\{([^}]*)\}/g, (_, before, body) => `${before}@media (prefers-color-scheme:dark){:root{${body}}}`);
  // And so is what a component draws otherwise in the dark (`dark:`),
  // written for an element under `.dark`.
  css = css.replace(/(?<=[{}]|^)([^{}@]*:is\(\.dark \*\)[^{}]*)\{([^{}]*)\}/g, (_, selector, body) => `@media (prefers-color-scheme:dark){${selector.replaceAll(":is(.dark *)", "")}{${body}}}`);
  return css;
}

const css = stylesheet();

function pack(name) {
  let html = readFileSync(join(out, "template", `${name}.html`), "utf8");
  html = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, "");
  // The still shipyard Next drew points at files the page will not have;
  // the script draws it instead.
  html = html.replace(/(<div[^>]*data-slot="shipyard"[^>]*>)(?:<img[^>]*>|<div[^>]*><\/div>)*<\/div>/g, "$1</div>");
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

async function bundle(name) {
  const result = await build({
    input: resolve(SCRIPTS[name]),
    moduleTypes: { ".svg": "dataurl" },
    output: { format: "iife", minify: true },
    write: false,
  });
  const code = result.output[0].code.trim();
  if (code.includes("</script")) throw new Error(`${name}: the script would end itself`);
  writeFileSync(join(target, `${name}.js`), code);
  return code.length;
}

for (const name of PAGES) console.log(name, pack(name), "bytes");
for (const name of Object.keys(SCRIPTS)) console.log(`${name}.js`, await bundle(name), "bytes");
