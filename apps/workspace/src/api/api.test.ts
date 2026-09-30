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

  it("keeps a change: a config var set is a release, and is listed after", async () => {
    const before = (await api.project("hello-world")).release;
    await api.changeConfigVars("hello-world", { set: { API_KEY: "x" } });
    const vars = await api.configVars("hello-world");
    expect(vars.vars.map((v) => v.name)).toContain("API_KEY");
    expect((await api.project("hello-world")).release).toBe(before + 1);
  });

  it("has no socket for the shell: the screen makes one up", async () => {
    expect(await api.shellSocket("hello-world", "web-1", "mock")).toBeNull();
  });
});
