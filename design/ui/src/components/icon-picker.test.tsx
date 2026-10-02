import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { IconPicker } from "./icon-picker";

describe("IconPicker", () => {
  it("chooses a symbol and a colour, each one at a time", () => {
    const onChange = vi.fn();
    render(<IconPicker value={{ icon: "briefcase", colour: "teal" }} onChange={onChange} />);
    const symbols = screen.getByRole("radiogroup", { name: "Symbol" });
    expect(within(symbols).getByRole("radio", { name: "briefcase" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(within(symbols).getByRole("radio", { name: "truck" }));
    expect(onChange).toHaveBeenLastCalledWith({ icon: "truck", colour: "teal", file: undefined });
    fireEvent.click(screen.getByRole("radio", { name: "amber" }));
    expect(onChange).toHaveBeenLastCalledWith({ icon: "briefcase", colour: "amber" });
  });

  it("finds a symbol by its name or by its group", () => {
    render(<IconPicker value={{}} onChange={() => {}} />);
    const search = screen.getByRole("textbox", { name: "Search the symbols" });
    fireEvent.change(search, { target: { value: "chart" } });
    const symbols = screen.getByRole("radiogroup", { name: "Symbol" });
    expect(within(symbols).getAllByRole("radio").map((b) => b.getAttribute("aria-label"))).toEqual(["chart-line", "chart-column", "chart-pie"]);
    fireEvent.change(search, { target: { value: "money" } });
    expect(within(symbols).getAllByRole("radio")).toHaveLength(8);
    fireEvent.change(search, { target: { value: "nothing-like-it" } });
    expect(screen.getByText("No symbol is called that.")).not.toBeNull();
  });

  it("takes an SVG of its own with its type, even when the system gave it none", async () => {
    const onChange = vi.fn();
    const { container } = render(<IconPicker value={{ icon: "briefcase" }} onChange={onChange} />);
    const input = container.querySelector<HTMLInputElement>("input[type=file]")!;
    fireEvent.change(input, { target: { files: [new File(["<svg/>"], "mark.svg", { type: "" })] } });
    await waitFor(() => expect(onChange).toHaveBeenCalled());
    const next = onChange.mock.calls[0][0];
    expect(next.file.type).toBe("image/svg+xml");
    expect(next.file.src.startsWith("data:image/svg+xml;base64,")).toBe(true);
  });

  it("draws an image of its own in place of the symbol, and lets it go", () => {
    const onChange = vi.fn();
    const { container } = render(
      <IconPicker value={{ icon: "briefcase", colour: "teal", file: { src: "/own.png", type: "image/png" } }} onChange={onChange} />,
    );
    expect(container.querySelector("[data-slot=launcher-tile]")?.getAttribute("data-picture")).toBe("true");
    expect(within(screen.getByRole("radiogroup", { name: "Symbol" })).getByRole("radio", { name: "briefcase" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText(/the colour is for a symbol/)).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(onChange).toHaveBeenLastCalledWith({ icon: "briefcase", colour: "teal", file: undefined });
  });
});
