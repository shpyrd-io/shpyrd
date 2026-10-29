import { describe, expect, it } from "vitest";
import { api } from "./api";

// NEXT_PUBLIC_API_MODE is "mock" in the tests: every call goes to the Mock.
describe("the api picks the Mock when asked to", () => {
  it("answers the config and the projects from the JSON files", async () => {
    const config = await api.config();
    expect(config.workspace?.slug).toBe("acme");
    const projects = await api.projects();
    expect(projects.map((p) => p.slug)).toContain("hello-world");
  });

  it("saves config changes without a release until redeploy", async () => {
    const before = (await api.project("hello-world")).release;
    await api.changeConfigVars("hello-world", { set: { API_KEY: "x" } });
    const vars = await api.configVars("hello-world");
    expect(vars.vars.map((v) => v.name)).toContain("API_KEY");
    expect(vars.pending).toBe(true);
    expect((await api.project("hello-world")).release).toBe(before);
    await api.redeploy("hello-world");
    expect((await api.project("hello-world")).release).toBe(before + 1);
    expect((await api.configVars("hello-world")).pending).toBe(false);
  }, 15_000);

  it("shows plain variables but never exposes saved secrets", async () => {
    await api.changeConfigVars("hello-world", { kind: "variable", set: { APP_URL: "https://example.test" } });
    expect((await api.configVars("hello-world")).variables).toContainEqual(expect.objectContaining({ name: "APP_URL", value: "https://example.test" }));
    await api.changeConfigVars("hello-world", { set: { AUTH_TOKEN: "private" } });
    await expect(api.changeConfigVars("hello-world", { kind: "variable", convert: ["AUTH_TOKEN"] })).rejects.toThrow(/cannot be made visible/);
    expect(JSON.stringify(await api.configVars("hello-world"))).not.toContain("private");
    await expect(api.changeConfigVars("hello-world", { kind: "variable", convert: ["AUTH_TOKEN"], confirmPlain: true })).rejects.toThrow(/cannot be made visible/);
    await expect(api.changeConfigVars("hello-world", { kind: "variable", set: { AUTH_TOKEN: "replacement" }, confirmPlain: true })).rejects.toThrow(/cannot be made visible/);
    expect(JSON.stringify(await api.configVars("hello-world"))).not.toContain("private");
    await api.changeConfigVars("hello-world", { kind: "secret", convert: ["AUTH_TOKEN"] });
    expect(JSON.stringify(await api.configVars("hello-world"))).not.toContain("private");
  }, 15_000);

  it("lists each project with the teams that have access to it and the domain of its own that answers", async () => {
    const projects = await api.projects();
    const hello = projects.find((p) => p.slug === "hello-world")!;
    expect(hello.teams).toEqual(["support"]);
    expect(hello.domain).toBe("hello.acme.com");
    expect(projects.find((p) => p.slug === "billing")?.teams).toEqual(["finance", "support"]);
  });

  it("keeps the icon of a card: a symbol and its colour, or an image of its own until it is removed", async () => {
    await api.updateProject("docs", { icon: "truck", iconColor: "amber" });
    let docs = await api.project("docs");
    expect([docs.icon, docs.iconColor]).toEqual(["truck", "amber"]);
    docs = await api.setProjectIcon("docs", "data:image/png;base64,iVBORw0KGgo=");
    expect(docs.iconType).toBe("image/png");
    await expect(api.setProjectIcon("docs", "data:image/gif;base64,R0lGOD")).rejects.toThrow(/SVG, a PNG or a WebP/);
    docs = await api.removeProjectIcon("docs");
    expect(docs.iconUrl).toBeUndefined();
  });

  it("has no socket for the shell: the screen makes one up", async () => {
    expect(await api.shellSocket("hello-world", "web-1", "mock")).toBeNull();
  });

  it("switches auto-deploy of a project on a branch, and refuses it for one that is not", async () => {
    const billing = await api.updateProject("billing", { autoDeploy: true });
    expect(billing.spec.source?.git?.autoDeploy).toBe(true);
    await expect(api.updateProject("dash-tv", { autoDeploy: false })).rejects.toThrow(/git repository/);
    expect((await api.projectGitHooks("billing")).url).toContain("/api/hooks/git/billing/");
  });

  it("keeps watch paths and wait for CI on a project on a branch, and refuses a pattern it cannot read", async () => {
    const hello = await api.updateProject("hello-world", { watchPaths: ["services/api/**", "!**/*.md"], waitForChecks: true });
    expect(hello.spec.source?.git?.watchPaths).toEqual(["services/api/**", "!**/*.md"]);
    expect(hello.spec.source?.git?.waitForChecks).toBe(true);
    expect(hello.githubFeatures).toBe(true);
    await expect(api.updateProject("hello-world", { watchPaths: ["a**"] })).rejects.toThrow(/stands alone/);
    await expect(api.updateProject("hello-world", { watchPaths: ["é".repeat(201)] })).rejects.toThrow(/at most 200 characters/);
    await expect(api.updateProject("hello-world", { watchPaths: ["é".repeat(200)] })).resolves.toBeTruthy();
    await expect(api.updateProject("hello-world", { watchPaths: Array.from({ length: 21 }, (_, i) => `d${i}/**`) })).rejects.toThrow(/at most 20 watch paths; there are 21/);
    await expect(api.updateProject("hello-world", { watchPaths: ["src/[a"] })).rejects.toThrow(
      'the watch path "src/[a" is not a pattern shpyrd can read: check its [ ] and \\',
    );
    await expect(api.updateProject("hello-world", { watchPaths: ["src/a\\"] })).rejects.toThrow(/not a pattern shpyrd can read/);
    await expect(api.updateProject("hello-world", { watchPaths: ["src/[ab]*"] })).resolves.toBeTruthy();
    await expect(api.updateProject("dash-tv", { waitForChecks: true })).rejects.toThrow(/git repository/);
  });

  it("reads a character class in a watch path as Go's path.Match does, and trims a pattern before checking it", async () => {
    // What Go's path.Match answers for each (checked with go run).
    for (const bad of ["src/[a-]", "src/[-a]", "src/[a-b-c]", "src/[--a]"])
      await expect(api.updateProject("hello-world", { watchPaths: [bad] })).rejects.toThrow(/not a pattern shpyrd can read/);
    for (const good of ["src/[a-z]", "src/[\\]]", "src/[^a]", "src/[\\-a]"])
      await expect(api.updateProject("hello-world", { watchPaths: [good] })).resolves.toBeTruthy();
    // Trimmed as the server does, before the check.
    expect((await api.updateProject("hello-world", { watchPaths: [" a/** "] })).spec.source?.git?.watchPaths).toEqual(["a/**"]);
  });

  it("knows the source of the projects on a branch: pinned, skipped, recorded only, unreadable", async () => {
    expect((await api.project("hello-world")).status.source?.commit).toMatch(/^[0-9a-f]{40}$/);
    expect((await api.project("docs")).status.source?.skipped).toBeTruthy();
    expect((await api.project("billing")).status.source?.latest).not.toBe((await api.project("billing")).status.source?.commit);
    expect((await api.project("reports")).status.source?.error).toContain("shpyrd git-auth");
  });

  it("links the person's GitHub in the Mock, connects an account, lists what it reaches and forgets a connection", async () => {
    await expect(api.githubAccounts()).rejects.toMatchObject({ status: 409, code: "github_link_required" });
    await expect(api.githubBranches("acme/hello-world")).rejects.toMatchObject({ status: 409, code: "github_link_required" });
    expect(await api.githubMe()).toBeNull();
    expect((await api.linkGitHub("/")).authorizeURL).toBe("");
    expect((await api.githubMe())?.login).toBe("ana-souza");
    const before = await api.github();
    expect(before.configured).toBe(true);
    const { installURL, authorizeURL } = await api.connectGitHub();
    expect(installURL).toBe("/workspace/git");
    expect(authorizeURL).toBe("/workspace/git");
    const accounts = await api.githubAccounts();
    expect(accounts.connected).toBe(before.installations.length + 1);
    expect(accounts.accounts.map((a) => a.account)).toEqual(["acme", "acme-labs"]);
    expect((await api.githubRepositories(42)).map((r) => r.fullName)).toContain("acme/hello-world");
    expect((await api.githubRepositories(77)).map((r) => r.fullName)).toEqual(["acme-labs/ml-pipeline"]);
    expect(await api.githubBranches("acme/hello-world")).toContain("main");
    await expect(api.githubBranches("someone/else")).rejects.toThrow(/No GitHub connection/);
    await api.disconnectGitHub(77);
    expect((await api.github()).installations.length).toBe(before.installations.length);
    expect((await api.projectGitAuth("hello-world")).using).toBe("github-app");
    await api.unlinkGitHub();
    expect(await api.githubMe()).toBeNull();
  }, 15_000);
});

// The Mock judges like the server (storage plan step 4): the plan's
// switch, the workspace's ceiling, the project's own cap, in its words.
describe("the mock holds the workspace to its ceilings", () => {
  it("refuses a disk past the project's cap and past the workspace's ceiling, in the server's words", async () => {
    await expect(api.createVolume("hello-world", { name: "huge", size: "1000Gi" })).rejects.toThrow("This would bring the workspace to 1120 GB of 500 GB.");
    await api.putProjectCaps({ "hello-world": { storage: "25Gi" } });
    await expect(api.createVolume("hello-world", { name: "big", size: "10Gi" })).rejects.toThrow("This would bring the project to 40 GB of 25 GB.");
    const usage = await api.projectUsage("hello-world");
    expect(usage).toEqual({ storage: "30Gi", ceiling: "25Gi", own: true });
    await api.putProjectCaps({});
    expect((await api.projectUsage("hello-world")).own).toBe(false);
  }, 20_000);

  it("plays the free plan when the browser asks: no disks, one shared database", async () => {
    localStorage.setItem("shpyrd.mock.plan", "free");
    try {
      const ws = await api.workspace();
      expect(ws.limits?.allowDisks).toBe(false);
      expect((await api.config()).postgres?.types).toEqual(["shared"]);
      await expect(api.createVolume("hello-world", { name: "files", size: "1Gi" })).rejects.toThrow("Disks are not included in the free workspace; use a bucket for files, or upgrade.");
      await expect(api.createResource("hello-world", { kind: "Postgres", name: "second", spec: { type: "dedicated" } })).rejects.toThrow("Your plan includes a shared database. A dedicated database comes with a paid plan.");
      await expect(api.createResource("hello-world", { kind: "Postgres", name: "second", spec: {} })).rejects.toThrow("This would bring the workspace to 2 of 1 databases.");
    } finally {
      localStorage.removeItem("shpyrd.mock.plan");
    }
  }, 20_000);
});
