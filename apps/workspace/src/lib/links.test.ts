import { describe, expect, it } from "vitest";
import { bySection } from "./links";

describe("bySection", () => {
  it("groups links by section in the order they first appear, keeping their order", () => {
    const links = [
      { section: "Cloud", label: "Billing", url: "/.shpyrd/gate?name=billing" },
      { section: "Tools", label: "Status", url: "/.shpyrd/gate?name=status" },
      { section: "Cloud", label: "Support", url: "/.shpyrd/gate?name=support" },
    ];
    expect(bySection(links)).toEqual([
      ["Cloud", [links[0], links[2]]],
      ["Tools", [links[1]]],
    ]);
  });

  it("gives nothing for no links", () => {
    expect(bySection([])).toEqual([]);
  });
});
