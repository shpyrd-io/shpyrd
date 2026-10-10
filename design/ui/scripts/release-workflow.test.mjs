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

  it("publishes from a job that installs nothing but a pinned npm", () => {
    const publish = job("publish");
    expect(publish).toContain("id-token: write");
    expect(publish).toContain("npm publish");
    expect(publish).not.toMatch(/npm ci|npm install(?! --global npm@\d+\.\d+\.\d+\s)/);
  });

  it("leaves no credentials in the publishing job's checkout", () => {
    expect(job("publish")).toMatch(/actions\/checkout@[^\n]+\n\s+with:\n\s+persist-credentials: false/);
  });

  it("never makes a library release the repository's latest, which the CLI's install script follows", () => {
    expect(job("publish")).toMatch(/gh release create[^\n]*--latest=false/);
  });

  it("publishes to npm only what npm does not have, so a re-run can finish a half-done release", () => {
    expect(job("publish")).toMatch(/- name: Publish\n\s+if: steps\.plan\.outputs\.publish == 'true'/);
  });

  it("makes the GitHub release only when it is missing, so a re-run does not fail on it", () => {
    expect(job("publish")).toMatch(/gh release view "\$TAG"[^\n]*\|\|\s*gh release create/);
  });
});
