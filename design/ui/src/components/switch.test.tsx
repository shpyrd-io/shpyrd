import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Label } from "./label";
import { Switch } from "./switch";

describe("Switch", () => {
  it("is a switch, and says its state in a word", () => {
    render(<Switch aria-label="Metrics" />);
    expect(screen.getByRole("switch", { name: "Metrics" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText("Off")).not.toBeNull();
  });

  it("turns on when pressed, and tells", () => {
    const onCheckedChange = vi.fn();
    render(<Switch aria-label="Metrics" onCheckedChange={onCheckedChange} />);
    fireEvent.click(screen.getByRole("switch"));
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText("On")).not.toBeNull();
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("follows what it is told when controlled", () => {
    render(<Switch aria-label="Mail" checked />);
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText("On")).not.toBeNull();
  });

  it("waits while what was asked is on its way", () => {
    render(<Switch aria-label="Mail" loading />);
    const s = screen.getByRole("switch") as HTMLButtonElement;
    expect(s.disabled).toBe(true);
    expect(s.getAttribute("aria-busy")).toBe("true");
  });

  it("takes its name from a label that points to it", () => {
    render(
      <>
        <Label htmlFor="password">Email and password sign-in</Label>
        <Switch id="password" />
      </>,
    );
    expect(screen.getByRole("switch", { name: "Email and password sign-in" })).not.toBeNull();
  });

  it("may hide the word, or put it at the end", () => {
    const { container } = render(
      <>
        <Switch aria-label="A" statusLabel={false} />
        <Switch aria-label="B" statusLabelPosition="end" />
      </>,
    );
    const switches = container.querySelectorAll("[data-slot=switch]");
    expect(switches[0].querySelector("[data-slot=switch-status]")).toBeNull();
    expect(switches[1].className).toContain("flex-row-reverse");
  });
});
