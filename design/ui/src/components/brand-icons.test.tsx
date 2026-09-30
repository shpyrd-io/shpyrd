import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ClaudeIcon, OpenAIIcon, WarpIcon } from "./brand-icons";

describe("the marks of the AI tools", () => {
  it("is an image named after the tool, filled in the colour of its brand, the size of an icon", () => {
    const { container } = render(<ClaudeIcon />);
    expect(screen.getByRole("img", { name: "Claude" })).not.toBeNull();
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("fill")).toBe("#D97757");
    expect(svg.className.baseVal).toContain("size-4");
  });

  it("takes the colour of the text when the brand's mark is black, so it holds in the dark", () => {
    const { container } = render(<OpenAIIcon />);
    expect(container.querySelector("svg")!.getAttribute("fill")).toBe("currentColor");
  });

  it("takes another size from its class, and each has a path of its own", () => {
    const { container } = render(
      <>
        <OpenAIIcon className="size-8" />
        <WarpIcon />
      </>,
    );
    const [openai, warp] = Array.from(container.querySelectorAll("svg"));
    expect(openai!.className.baseVal).toContain("size-8");
    expect(openai!.querySelector("path")!.getAttribute("d")).not.toBe(warp!.querySelector("path")!.getAttribute("d"));
  });

  it("is drawn in strokes when asked as a line: its own lines, not the filled shape, at the weight of lucide", () => {
    const { container } = render(<ClaudeIcon variant="line" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("fill")).toBe("none");
    expect(svg.getAttribute("stroke")).toBe("currentColor");
    expect(svg.getAttribute("stroke-width")).toBe("2");
    expect(svg.getAttribute("data-variant")).toBe("line");
    expect(svg.querySelectorAll("path").length).toBe(12);
    const filled = render(<ClaudeIcon />).container.querySelector("path")!.getAttribute("d");
    expect(svg.querySelector("path")!.getAttribute("d")).not.toBe(filled);
  });
});
