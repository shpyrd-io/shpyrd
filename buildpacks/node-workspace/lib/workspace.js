// Reads the workspace for bin/build: its package manager, by lockfile, and
// the package BP_NODE_WORKSPACE names. Prints shell assignments; on a
// mistake, a sentence on stderr and exit 1. Node's own modules only.
"use strict";
const fs = require("node:fs");
const path = require("node:path");

function fail(message) {
  process.stderr.write(message + "\n");
  process.exit(1);
}
function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}
function quote(value) {
  return "'" + String(value).replace(/'/g, "'\\''") + "'";
}

const asked = process.argv[2] || "";
const want = asked.replace(/^(\.\/)+/, "").replace(/\/+$/, "");
if (!want || want.startsWith("/") || want.split("/").some((s) => s === "" || s === "." || s === ".."))
  fail(`build.workspace must be a package's path inside the repository, like apps/web; got "${asked}"`);

let manager;
if (fs.existsSync("pnpm-lock.yaml")) manager = "pnpm";
else if (fs.existsSync("yarn.lock")) manager = /^__metadata:/m.test(fs.readFileSync("yarn.lock", "utf8")) ? "yarn" : "yarn1";
else if (fs.existsSync("package-lock.json")) manager = "npm";
else fail("no lockfile at the workspace's root; commit the one your package manager writes (package-lock.json, pnpm-lock.yaml or yarn.lock)");

const root = readJSON("package.json") || {};
const patterns =
  manager === "pnpm"
    ? pnpmPackages()
    : Array.isArray(root.workspaces)
      ? root.workspaces
      : (root.workspaces && root.workspaces.packages) || [];

// pnpm-workspace.yaml's packages, as a block list or a flow list.
function pnpmPackages() {
  let text;
  try {
    text = fs.readFileSync("pnpm-workspace.yaml", "utf8");
  } catch {
    return [];
  }
  const unquote = (s) => s.trim().replace(/^['"]|['"]$/g, "");
  const out = [];
  let inList = false;
  for (const raw of text.split("\n")) {
    const line = raw.replace(/\s+#.*$/, "");
    const flow = line.match(/^packages:\s*\[(.*)\]\s*$/);
    if (flow) return flow[1].split(",").map(unquote).filter(Boolean);
    if (/^packages:\s*$/.test(line)) {
      inList = true;
      continue;
    }
    if (!inList) continue;
    const item = line.match(/^\s+-\s+(.+)$/);
    if (item) out.push(unquote(item[1]));
    else if (/^\S/.test(line)) inList = false;
  }
  return out;
}

// The managers' globs: "*" and "?" within a segment, "**" any number of
// segments, a leading "!" excludes. The CLI matches the same way
// (internal/cli/workspace.go).
function matches(pattern, p) {
  const pat = pattern.replace(/^(\.\/)+/, "").replace(/\/+$/, "").split("/");
  return segments(pat, p.split("/"));
}
function segments(pat, segs) {
  if (pat.length === 0) return segs.length === 0;
  if (pat[0] === "**") {
    for (let i = 0; i <= segs.length; i++) if (segments(pat.slice(1), segs.slice(i))) return true;
    return false;
  }
  return segs.length > 0 && one(pat[0], segs[0]) && segments(pat.slice(1), segs.slice(1));
}
function one(glob, name) {
  let re = "^";
  for (const ch of glob) re += ch === "*" ? "[^/]*" : ch === "?" ? "[^/]" : ch.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  return new RegExp(re + "$").test(name);
}
function listed(p) {
  let yes = false;
  for (const pattern of patterns) {
    if (pattern.startsWith("!")) {
      if (matches(pattern.slice(1), p)) return false;
    } else if (matches(pattern, p)) yes = true;
  }
  return yes;
}

// Every folder of the workspace with a package.json that it lists, for
// the error message.
function packages(dir = ".", depth = 0, out = []) {
  if (depth > 4) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory() || entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const sub = dir === "." ? entry.name : `${dir}/${entry.name}`;
    if (fs.existsSync(path.join(sub, "package.json")) && listed(sub)) out.push(sub);
    packages(sub, depth + 1, out);
  }
  return out;
}

if (!listed(want)) {
  const there = packages();
  fail(`${want} is not a package of this workspace; its packages are: ${there.length ? there.join(", ") : "none"}`);
}
const pkg = readJSON(path.join(want, "package.json"));
if (!pkg) fail(`${want} has no package.json`);
if (manager !== "npm" && !pkg.name) fail(`${want}/package.json has no name, which ${manager.replace("1", "")} needs to select it`);
const scripts = pkg.scripts || {};

console.log(`manager=${quote(manager)}`);
console.log(`path=${quote(want)}`);
console.log(`name=${quote(pkg.name || "")}`);
console.log(`has_build=${scripts.build ? 1 : 0}`);
console.log(`has_start=${scripts.start ? 1 : 0}`);
console.log(`package_manager=${quote(root.packageManager || "")}`);
