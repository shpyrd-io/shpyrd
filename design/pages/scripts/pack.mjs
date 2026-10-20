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
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { build } from "rolldown";

const out = resolve("out");
const target = resolve("../../pkg/pages/html");
mkdirSync(target, { recursive: true });

const PAGES = ["nothing", "no-access", "waking", "mark", "service-unavailable", "consent", "cli-activate", "cli-done"];

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

// What every packed page drops of what Next wrote: its scripts, links and
// markers, and the escaping of the template's marks.
function strip(name) {
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
  return html;
}

function pack(name) {
  let html = strip(name);
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

// The pages Dex serves itself (src/dex.tsx), by the name of Dex's template
// each takes the place of, and what the browser's tab says. They go into
// the ConfigMap the Dex Deployment lays over the image's web tree, with
// the stylesheet as a file of the theme they link to; the Deployment is
// marked with their hash, so Dex starts again with them.
const DEX = {
  "dex-error": ["error.html", "Sign-in · shpyrd"],
  "dex-login": ["login.html", "Sign in · shpyrd"],
  "dex-password": ["password.html", "Sign in · shpyrd"],
  "dex-approval": ["approval.html", "Grant access · shpyrd"],
  "dex-device": ["device.html", "Sign in a device · shpyrd"],
  "dex-device-success": ["device_success.html", "Signed in · shpyrd"],
  "dex-oob": ["oob.html", "Sign-in code · shpyrd"],
};
const dexBase = resolve("../../deploy/components/dex/base");

function packDex() {
  const data = { "styles.css": css, "logo.svg": readFileSync(resolve("../brand/logo/logo-symbol.svg"), "utf8") };
  for (const [name, [file, title]] of Object.entries(DEX)) {
    data[file] = strip(name).replace(
      "</head>",
      `<title>${title}</title><link rel="stylesheet" href="{{url .ReqPath "theme/styles.css"}}"><link rel="icon" href="{{url .ReqPath "theme/logo.svg"}}"></head>`,
    );
  }
  for (const [file, text] of Object.entries(data)) {
    // The installer fills ${SHPYRD_…} in every manifest.
    if (text.includes("${SHPYRD_")) throw new Error(`${file}: would be read as an installer variable`);
  }
  // Each file by its name, in the order of the names (deploy/dex_web_test.go
  // counts it the same way).
  const sum = createHash("sha256");
  for (const file of Object.keys(data).sort()) sum.update(`${file}\0${data[file]}\0`);
  const hash = sum.digest("hex").slice(0, 16);
  const yaml = [
    "# Generated by design/pages (scripts/pack.mjs, `make pages`): do not edit.",
    "# The theme and templates of the pages Dex serves itself (src/dex.tsx);",
    "# the init container of the Deployment lays them over the image's tree.",
    "apiVersion: v1",
    "kind: ConfigMap",
    "metadata:",
    "  name: dex-web",
    "data:",
    ...Object.entries(data).map(([file, text]) => `  ${file}: ${JSON.stringify(text)}`),
    "",
  ].join("\n");
  writeFileSync(join(dexBase, "web.yaml"), yaml);
  const deployment = join(dexBase, "deployment.yaml");
  const before = readFileSync(deployment, "utf8");
  const after = before.replace(/(shpyrd\.io\/web: )"[^"]*"/, `$1"${hash}"`);
  if (after === before && !before.includes(`shpyrd.io/web: "${hash}"`)) throw new Error("deployment.yaml: no shpyrd.io/web annotation to mark");
  writeFileSync(deployment, after);
  return yaml.length;
}

for (const name of PAGES) console.log(name, pack(name), "bytes");
console.log("dex web.yaml", packDex(), "bytes");
for (const name of Object.keys(SCRIPTS)) console.log(`${name}.js`, await bundle(name), "bytes");
