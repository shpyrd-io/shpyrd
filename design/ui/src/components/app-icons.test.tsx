import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render } from "@testing-library/react";
import { AppIcon, AppSymbol, appIconGroups, appIcons, symbolColours, symbolInk, tileOf } from "./app-icons";

describe("app icons", () => {
  it("offers 64 symbols in eight groups of eight, each name once", () => {
    expect(appIconGroups).toHaveLength(8);
    for (const group of appIconGroups) expect(group.icons).toHaveLength(8);
    expect(Object.keys(appIcons)).toHaveLength(64);
  });

  it("draws the icon of a name, and nothing for a name not in the set", () => {
    const { container } = render(
      <>
        <AppIcon name="briefcase" />
        <AppIcon name="no-such-icon" />
      </>,
    );
    const icons = container.querySelectorAll("[data-slot=app-icon]");
    expect(icons).toHaveLength(1);
    expect(icons[0].tagName).toBe("svg");
  });

  it("fills a sent SVG with the colour of the text, through its shape", () => {
    const { container } = render(<AppSymbol src="/icon.svg" />);
    const symbol = container.querySelector<HTMLElement>("[data-slot=app-symbol]");
    expect(symbol?.className).toContain("bg-current");
    expect(symbol?.style.maskImage).toBe('url("/icon.svg")');
  });

  it("gives a tile the ink of a colour of the set, and none for any other", () => {
    expect(symbolInk("teal")).toEqual({ "--ink": "var(--symbol-teal)" });
    expect(symbolInk("#ff0000")).toBeUndefined();
    expect(symbolInk(undefined)).toBeUndefined();
  });

  it("has a token for every colour in both themes", () => {
    const css = readFileSync(resolve(__dirname, "../styles/index.css"), "utf8");
    const [light, dark] = css.split(/^\.dark \{$/m);
    for (const colour of symbolColours) {
      expect(light).toContain(`--symbol-${colour}:`);
      expect(dark).toContain(`--symbol-${colour}:`);
    }
  });

  it("draws a choice: an SVG of its own as a symbol, a PNG as a picture, else the symbol of its name", () => {
    expect(tileOf({ icon: "truck", file: { src: "/a.svg", type: "image/svg+xml" } }).icon?.props).toEqual({ src: "/a.svg" });
    expect(tileOf({ icon: "truck", file: { src: "/a.png", type: "image/png" } })).toEqual({ picture: "/a.png" });
    expect(tileOf({ icon: "truck" }).icon?.props).toEqual({ name: "truck" });
    expect(tileOf({ icon: "not-in-the-set" })).toEqual({});
  });
});
