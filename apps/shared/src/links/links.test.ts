import { describe, expect, it } from "vitest";
import { placeLinks, shown, type Link } from "./links";

const pages = [
  { group: "", slug: "" },
  { group: "Platform", slug: "workspaces" },
  { group: "Platform", slug: "accounts" },
  { group: "Cluster", slug: "sizes" },
];
const link = (l: Partial<Link>): Link => ({ section: "Platform", label: "x", url: "/x", ...l });
const layout = (groups: ReturnType<typeof placeLinks<(typeof pages)[number]>>) =>
  groups.map((g) => [g.group, g.entries.map((e) => (e.page ? e.page.slug : `>${e.link!.label}`))]);

describe("placeLinks", () => {
  it("puts a link after the page it names, even one hidden to the person", () => {
    const groups = shown(placeLinks(pages, [link({ label: "Workspaces", after: "workspaces" })]), (p) => p.slug !== "workspaces");
    expect(layout(groups)).toEqual([
      ["", [""]],
      ["Platform", [">Workspaces", "accounts"]],
      ["Cluster", ["sizes"]],
    ]);
  });

  it("puts a link at the end of its section without a page to follow", () => {
    expect(layout(placeLinks(pages, [link({ label: "End" }), link({ label: "Gone", after: "nothing" })]))[1]).toEqual(["Platform", ["workspaces", "accounts", ">End", ">Gone"]]);
  });

  it("keeps the server's order for links after the same page", () => {
    expect(layout(placeLinks(pages, [link({ label: "A", after: "workspaces" }), link({ label: "B", after: "workspaces" })]))[1]).toEqual(["Platform", ["workspaces", ">A", ">B", "accounts"]]);
  });

  it("puts a section the sidebar does not have after its own", () => {
    expect(layout(placeLinks(pages, [link({ section: "Cloud", label: "Billing" })])).map((g) => g[0])).toEqual(["", "Platform", "Cluster", "Cloud"]);
  });

  it("drops a group with nothing left to show", () => {
    expect(shown(placeLinks(pages, []), (p) => p.group !== "Cluster").map((g) => g.group)).toEqual(["", "Platform"]);
  });
});
