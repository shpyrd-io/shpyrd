// What a push to main releases of @shpyrd/ui: nothing, or a version with
// its dist-tag, its git tag and its notes (design/ui/README.md,
// "Releasing"). Run as a script in design/ui by ui-release.yml.
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

export function changelogSection(changelog, version) {
  const lines = changelog.split("\n");
  const start = lines.findIndex((l) => l.trim() === `## ${version}`);
  if (start < 0) return "";
  let end = lines.findIndex((l, i) => i > start && l.startsWith("## "));
  if (end < 0) end = lines.length;
  return lines.slice(start + 1, end).join("\n").trim();
}

export function releasePlan({ version, published, changelog }) {
  if (version === "0.0.0") {
    return { publish: false, reason: "0.0.0 is the version of a library never released" };
  }
  const release = { version, distTag: version.includes("-") ? "next" : "latest", gitTag: `ui-v${version}` };
  if (published.includes(version)) {
    // Still named: a run that published but failed before its GitHub
    // release is finished by running it again.
    return { publish: false, reason: `${version} is on npm already`, ...release, notes: changelogSection(changelog, version) };
  }
  const notes = changelogSection(changelog, version);
  if (!notes) {
    throw new Error(`CHANGELOG.md has no section for ${version}: write it before releasing`);
  }
  return { publish: true, ...release, notes };
}

// The versions npm has of the package: none for a package it has never
// seen. Any other failure stops the release rather than publish blind.
export function publishedVersions(name) {
  try {
    const out = execFileSync("npm", ["view", name, "versions", "--json"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    const versions = JSON.parse(out);
    return Array.isArray(versions) ? versions : [versions];
  } catch (err) {
    if (String(err.stderr ?? "").includes("E404")) return [];
    throw err;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const pkg = JSON.parse(readFileSync("package.json", "utf8"));
  const plan = releasePlan({
    version: pkg.version,
    published: pkg.version === "0.0.0" ? [] : publishedVersions(pkg.name),
    changelog: readFileSync("CHANGELOG.md", "utf8"),
  });
  const out = process.env.GITHUB_OUTPUT;
  console.log(plan.publish ? `releasing ${pkg.name}@${plan.version} under ${plan.distTag}, tagged ${plan.gitTag}` : `nothing to publish: ${plan.reason}`);
  // The outputs and the notes are the workflow's: run by hand, the script
  // leaves nothing.
  if (out) {
    appendFileSync(out, `publish=${plan.publish}\n`);
    if (plan.version) {
      const notesFile = join(process.env.RUNNER_TEMP ?? ".", "ui-release-notes.md");
      writeFileSync(notesFile, `${plan.notes}\n`);
      appendFileSync(out, `version=${plan.version}\ndist_tag=${plan.distTag}\ngit_tag=${plan.gitTag}\nnotes_file=${notesFile}\n`);
    }
  }
}
