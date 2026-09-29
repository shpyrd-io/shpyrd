import { afterEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { KeybindingHint } from "./keybinding-hint";

const on = (platform: string) =>
  Object.defineProperty(navigator, "platform", { value: platform, configurable: true });

// What is drawn of each chord, and what is read of the whole hint.
const drawn = (container: HTMLElement) =>
  [...container.querySelectorAll("[data-slot=kbd-chord]")].map((chord) =>
    [...chord.querySelectorAll("[aria-hidden]")].map((key) => key.textContent).join(" "),
  );
const read = (container: HTMLElement) =>
  [...container.querySelectorAll(".sr-only")].map((key) => key.textContent).join(" ");

afterEach(() => on(""));

describe("KeybindingHint", () => {
  it("draws Mod as Command on a Mac", () => {
    on("MacIntel");
    const { container } = render(<KeybindingHint keys="Mod+S" />);
    expect(drawn(container)).toEqual(["⌘ S"]);
    expect(read(container)).toBe("Command S");
  });

  it("draws Mod as Control on the others", () => {
    on("Win32");
    const { container } = render(<KeybindingHint keys="Mod+S" />);
    expect(drawn(container)).toEqual(["Ctrl S"]);
    expect(read(container)).toBe("Control S");
  });

  it("draws keys pressed in turn in a box each, with a then between them", () => {
    on("Win32");
    const { container } = render(<KeybindingHint keys="g i" />);
    expect(drawn(container)).toEqual(["G", "I"]);
    expect(read(container)).toBe("G then I");
  });

  it("puts the modifiers in the order of the platform", () => {
    on("MacIntel");
    expect(drawn(render(<KeybindingHint keys="Mod+Shift+P" />).container)).toEqual(["⇧ ⌘ P"]);
    on("Win32");
    expect(drawn(render(<KeybindingHint keys="Shift+Mod+P" />).container)).toEqual(["Ctrl ⇧ P"]);
  });

  it("writes the names of the keys in full, with a plus between them", () => {
    on("MacIntel");
    const { container } = render(<KeybindingHint keys="Mod+K" format="full" />);
    expect(container.querySelector("[data-slot=kbd-chord]")?.textContent).toBe("Command+K");
  });

  it("knows a key by its other names", () => {
    on("Win32");
    expect(drawn(render(<KeybindingHint keys="Ctrl+Alt+Del" />).container)).toEqual([
      "Ctrl Alt Del",
    ]);
    expect(drawn(render(<KeybindingHint keys="Esc" />).container)).toEqual(["Esc"]);
    expect(drawn(render(<KeybindingHint keys="Up" />).container)).toEqual(["↑"]);
  });

  it("says its size and what it is over", () => {
    const { container } = render(<KeybindingHint keys="Mod+K" size="sm" variant="onEmphasis" />);
    const hint = container.querySelector("kbd");
    expect(hint?.getAttribute("data-size")).toBe("sm");
    expect(hint?.getAttribute("data-variant")).toBe("onEmphasis");
  });
});

describe("a key it does not know", () => {
  it("is written as it was given", () => {
    on("Win32");
    expect(drawn(render(<KeybindingHint keys="Mod+F5" />).container)).toEqual(["Ctrl F5"]);
  });
});
