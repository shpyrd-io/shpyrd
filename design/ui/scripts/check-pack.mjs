// What npm would publish of @shpyrd/ui: the components, the styles and the
// papers; nothing of the gallery, the tests, the scripts or the
// configuration. Run in design/ui.
import { execFileSync } from "node:child_process";

const [pack] = JSON.parse(execFileSync("npm", ["pack", "--dry-run", "--json"], { encoding: "utf8" }));
const files = pack.files.map((f) => f.path);

const forbidden = files.filter((f) =>
  /\.test\.tsx?$|^src\/test\.ts$|^app\/|^public\/|^scripts\/|^next\.config|^vitest\.config|^tsconfig|^postcss\.config|^next-env\.d\.ts$|\.tsbuildinfo$/.test(f),
);
const required = ["package.json", "README.md", "CHANGELOG.md", "LICENSE", "src/styles/index.css", "src/components/button.tsx"];
const missing = required.filter((f) => !files.includes(f));

if (forbidden.length || missing.length) {
  for (const f of forbidden) console.error(`ships but must not: ${f}`);
  for (const f of missing) console.error(`must ship but does not: ${f}`);
  process.exit(1);
}
console.log(`${pack.name} would ship ${files.length} files, none of them the gallery's, a test or configuration`);
