import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// The release workflow, read as text and cut into its jobs: the publishing
// job is the one that can publish (id-token) and write the repository, so
// it must run no code it did not choose (#130's supply-chain review).
// Vitest runs from design/ui, the library's root.
const workflow = readFileSync(join(process.cwd(), "../../.github/workflows/ui-release.yml"), "utf8");

function job(name) {
  const lines = workflow.split("\n");
  const start = lines.indexOf(`  ${name}:`);
  if (start < 0) return "";
  let end = lines.findIndex((l, i) => i > start && /^ {2}[a-z][\w-]*:\s*$/.test(l));
  if (end < 0) end = lines.length;
  return lines.slice(start, end).join("\n");
}

describe("the release workflow", () => {
  it("gives the workflow as a whole no more than reading", () => {
    const top = workflow.slice(0, workflow.indexOf("\njobs:"));
    expect(top).toMatch(/permissions:\n {2}contents: read\n/);
    expect(top).not.toMatch(/id-token/);
  });

  it("runs the dependencies' code only in a job that can neither publish nor write", () => {
    const gates = job("gates");
    expect(gates).toContain("npm ci");
    expect(gates).not.toMatch(/id-token|contents: write/);
  });

  it("stages from a job that installs nothing but a pinned npm", () => {
    const publish = job("publish");
    expect(publish).toContain("id-token: write");
    expect(publish).toContain("npm stage publish");
    expect(publish).not.toMatch(/npm ci|npm install(?! --global npm@\d+\.\d+\.\d+\s)/);
  });

  it("only stages: a version reaches npm when a maintainer approves it, never from the workflow alone", () => {
    const commands = workflow.split("\n").filter((l) => !l.trim().startsWith("#")).join("\n");
    expect(commands).not.toMatch(/npm publish/);
  });

  it("leaves no credentials in the publishing job's checkout", () => {
    expect(job("publish")).toMatch(/actions\/checkout@[^\n]+\n\s+with:\n\s+persist-credentials: false/);
  });

  it("never makes a library release the repository's latest, which the CLI's install script follows", () => {
    expect(job("publish")).toMatch(/gh release create[^\n]*--latest=false/);
  });

  it("stages only what npm does not have", () => {
    expect(job("publish")).toMatch(/- name: Stage on npm\n\s+if: steps\.plan\.outputs\.publish == 'true'/);
  });

  it("tags the run's own commit, the one npm gets, before staging it", () => {
    const publish = job("publish");
    expect(publish).toMatch(/- name: Tag the commit npm gets\n\s+if: steps\.plan\.outputs\.publish == 'true'/);
    expect(publish).toContain('-f sha="$GITHUB_SHA"');
    expect(publish.indexOf("- name: Tag the commit npm gets")).toBeLessThan(publish.indexOf("- name: Stage on npm"));
  });

  it("packs once, in the job that installs nothing, and checks what ships there", () => {
    const publish = job("publish");
    expect(publish).toMatch(/node scripts\/check-pack\.mjs\n\s+mkdir -p "\$RUNNER_TEMP\/pack"\n\s+file=\$\(npm pack /);
    expect(publish.match(/npm pack /g)).toHaveLength(1);
  });

  it("gives the GitHub release and npm the same tarball, from the tag on this commit", () => {
    const publish = job("publish");
    expect(publish).toMatch(/gh release create "\$TAG" "\$TARBALL"[^\n]*--verify-tag/);
    expect(publish).not.toMatch(/gh release create[^\n]*--target/);
    expect(publish).toMatch(/npm stage publish "\$TARBALL"/);
  });

  it("stages last, the one step a re-run cannot repeat", () => {
    const publish = job("publish");
    const order = ["- name: Pack", "- name: Tag the commit npm gets", "- name: GitHub release", "- name: Stage on npm"].map((s) => publish.indexOf(s));
    expect(order.every((at) => at >= 0)).toBe(true);
    expect([...order].sort((x, y) => x - y)).toEqual(order);
  });

  it("makes the GitHub release only when it is missing, so a re-run does not fail on it", () => {
    expect(job("publish")).toMatch(/gh release view "\$TAG"[^\n]*&& exit 0\n[\s\S]*gh release create/);
  });
});
