import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { changelogSection, publishedVersions, releasePlan } from "./release-plan.mjs";

const changelog = `# Changelog of @shpyrd/ui

## 0.2.0

CodeInput takes a length.

## 0.1.0

The first published version.
`;

describe("releasePlan", () => {
  it("releases nothing while the version is the unreleased 0.0.0", () => {
    expect(releasePlan({ version: "0.0.0", published: [], changelog }).publish).toBe(false);
  });

  it("releases nothing when npm has the version already", () => {
    const plan = releasePlan({ version: "0.2.0", published: ["0.1.0", "0.2.0"], changelog });
    expect(plan.publish).toBe(false);
    expect(plan.reason).toContain("0.2.0");
  });

  it("releases a new version under latest, tagged ui-v, with its section as notes", () => {
    expect(releasePlan({ version: "0.2.0", published: ["0.1.0"], changelog })).toEqual({
      publish: true,
      version: "0.2.0",
      distTag: "latest",
      gitTag: "ui-v0.2.0",
      notes: "CodeInput takes a length.",
    });
  });

  it("releases a version with a suffix under next, never latest", () => {
    const withRc = `${changelog}\n## 0.3.0-rc.1\n\nA try.\n`;
    expect(releasePlan({ version: "0.3.0-rc.1", published: [], changelog: withRc }).distTag).toBe("next");
  });

  it("refuses to release a version the CHANGELOG says nothing about", () => {
    expect(() => releasePlan({ version: "0.4.0", published: [], changelog })).toThrow(/CHANGELOG\.md has no section for 0\.4\.0/);
  });
});

describe("changelogSection", () => {
  it("is the text of the version's section, up to the next one", () => {
    expect(changelogSection(changelog, "0.1.0")).toBe("The first published version.");
  });

  it("is empty for a version with no section", () => {
    expect(changelogSection(changelog, "9.9.9")).toBe("");
  });
});

describe("publishedVersions", () => {
  const path = process.env.PATH;
  afterEach(() => {
    process.env.PATH = path;
  });

  // An npm that answers as the registry would, first on the PATH.
  function npmAnswering(stderr, code) {
    const dir = mkdtempSync(join(tmpdir(), "npm-"));
    writeFileSync(join(dir, "npm"), `#!/bin/sh\necho '${stderr}' >&2\nexit ${code}\n`);
    chmodSync(join(dir, "npm"), 0o755);
    process.env.PATH = `${dir}:${path}`;
  }

  it("is none for a package npm has never seen", () => {
    npmAnswering("npm error code E404", 1);
    expect(publishedVersions("@shpyrd/ui")).toEqual([]);
  });

  it("stops the release when npm does not answer, rather than publish blind", () => {
    npmAnswering("npm error code ETIMEDOUT", 1);
    expect(() => publishedVersions("@shpyrd/ui")).toThrow();
  });
});
