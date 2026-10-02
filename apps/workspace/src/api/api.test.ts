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
});
