import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { IDE } from "./ide";

const files = [
  { name: "server.js", code: 'const port = 3000;\n// the port\n' },
  { name: "shpyrd.yaml", code: "name: hello-world\n" },
];

const lines = (container: HTMLElement) => container.querySelectorAll("code > span").length;

describe("IDE", () => {
  it("shows the first file, under the names of all", () => {
    const { container } = render(<IDE files={files} />);
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual([
      "server.js",
      "shpyrd.yaml",
    ]);
    expect(screen.getByRole("tab", { name: "server.js" }).getAttribute("aria-selected")).toBe(
      "true",
    );
    expect(screen.getByRole("tabpanel").textContent).toContain("const port = 3000;");
    expect(lines(container)).toBe(2);
  });

  it("shows another file when its name is pressed, or reached with the arrows", () => {
    render(<IDE files={files} />);
    fireEvent.click(screen.getByRole("tab", { name: "shpyrd.yaml" }));
    expect(screen.getByRole("tabpanel").textContent).toContain("name: hello-world");

    fireEvent.keyDown(screen.getByRole("tablist"), { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "server.js" }).getAttribute("aria-selected")).toBe(
      "true",
    );
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "server.js" }));
  });

  it("has no names over code that is given by itself", () => {
    const { container } = render(<IDE code={"shpyrd login\n"} language="sh" />);
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.getByRole("region", { name: "Code" }).textContent).toContain("shpyrd login");
    expect(screen.getByRole("button", { name: "Copy the code" })).toBeTruthy();
    expect(container.firstElementChild?.getAttribute("data-tabs")).toBe("false");
  });

  it("hides the name of a file when it is told to", () => {
    render(<IDE files={[files[0]]} tabs={false} />);
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.getByRole("button", { name: "Copy server.js" })).toBeTruthy();
  });

  it("numbers the lines, unless it is told not to", () => {
    const numbers = (container: HTMLElement) =>
      [...container.querySelectorAll("code > span > span[aria-hidden]")].map((n) => n.textContent);
    expect(numbers(render(<IDE files={files} />).container)).toEqual(["1", "2"]);
    expect(numbers(render(<IDE files={files} showLineNumbers={false} />).container)).toEqual([]);
  });

  it("does not count the end of the last line as one more line", () => {
    expect(lines(render(<IDE code={"a\nb\n"} />).container)).toBe(2);
    expect(lines(render(<IDE code={"a\n\nb"} />).container)).toBe(3);
  });

  it("colours the code by the language read from the name of the file", () => {
    const { container } = render(<IDE files={files} />);
    expect(container.querySelector(".font-semibold")?.textContent).toBe("const");
    expect(container.querySelector(".italic")?.textContent).toBe("// the port");
  });

  it("copies the code of the file that is shown, and says so", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<IDE files={files} />);
    fireEvent.click(screen.getByRole("tab", { name: "shpyrd.yaml" }));
    fireEvent.click(screen.getByRole("button", { name: "Copy shpyrd.yaml" }));
    expect(writeText).toHaveBeenCalledWith("name: hello-world\n");
    await waitFor(() => expect(screen.getByRole("button", { name: "Copied" })).toBeTruthy());
  });

  it("is no taller than it is told, and scrolls what does not fit", () => {
    render(<IDE files={files} height={200} />);
    expect(screen.getByRole("tabpanel").style.maxHeight).toBe("200px");
  });

  it("draws nothing when it is given nothing", () => {
    expect(render(<IDE />).container.firstElementChild).toBeNull();
  });
});
