import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ProgressBar } from "./progress-bar";

describe("ProgressBar", () => {
  it("says how far it has gone, of how much", () => {
    render(<ProgressBar label="Build" value={25} />);
    const bar = screen.getByRole("progressbar", { name: "Build" });
    expect(bar.getAttribute("aria-valuenow")).toBe("25");
    expect(bar.getAttribute("aria-valuemax")).toBe("100");
    expect(screen.getByText("25%")).not.toBeNull();
  });

  it("fills its share and outlines what is left", () => {
    render(<ProgressBar aria-label="Upload" value={62} />);
    const parts = screen.getByRole("progressbar").children;
    expect(parts).toHaveLength(2);
    expect((parts[0] as HTMLElement).style.width).toBe("62%");
    expect((parts[1] as HTMLElement).style.width).toBe("38%");
  });

  it("goes no further than the whole", () => {
    render(<ProgressBar aria-label="Over" value={140} />);
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuenow")).toBe("100");
    expect(bar.children).toHaveLength(1);
  });

  it("is made of parts, each with a name", () => {
    render(
      <ProgressBar
        label="Tests"
        max={10}
        segments={[
          { label: "Passed", value: 7 },
          { label: "Failed", value: 1, tone: "error" },
        ]}
      />,
    );
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("8");
    expect(screen.getByText("Passed")).not.toBeNull();
    expect(screen.getByText("Failed")).not.toBeNull();
    expect(screen.getByText("80%")).not.toBeNull();
  });

  it("is a span inline, so that it may sit in a line of text", () => {
    const { container } = render(
      <p>
        done <ProgressBar inline aria-label="Release" value={62} />
      </p>,
    );
    expect(container.querySelector("div")).toBeNull();
    expect(screen.getByRole("progressbar").tagName).toBe("SPAN");
  });
});
