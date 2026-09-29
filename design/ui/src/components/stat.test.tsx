import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Sparkline, Stat } from "./stat";

describe("Stat", () => {
  it("needs only what it is and how much", () => {
    const { container } = render(<Stat label="Projects" value="12" />);
    expect(screen.getByText("Projects")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    expect(container.querySelector("[data-slot=sparkline]")).toBeNull();
  });

  it("writes what the value is counted in after it", () => {
    const { container } = render(<Stat label="Response time" value="223" unit="ms" />);
    expect(container.querySelector(".font-heading")?.textContent).toBe("223ms");
  });

  it("says whether a change is good with a colour and a direction, not a colour alone", () => {
    const { container } = render(
      <Stat
        label="Failed requests"
        value="1.4"
        delta={{ value: "0.6", direction: "up", good: false, against: "against yesterday" }}
      />,
    );
    const change = screen.getByText("0.6");
    expect(change.className).toContain("text-destructive");
    expect(change.querySelector("svg")).toBeTruthy();
    expect(container.textContent).toContain("against yesterday");
  });

  it("takes the colour of a good change from a name", () => {
    render(<Stat label="a" value="1" delta={{ value: "12%", direction: "down", good: true }} />);
    expect(screen.getByText("12%").className).toContain("text-success");
  });

  it("draws how the value went lately", () => {
    const { container } = render(<Stat label="a" value="1" trend={[1, 3, 2]} />);
    expect(container.querySelector("[data-slot=sparkline] svg")).toBeTruthy();
  });
});

describe("Sparkline", () => {
  const edge = (values: number[], kind?: "step" | "line") =>
    render(<Sparkline values={values} kind={kind} />).container
      .querySelector("path[fill=none]")
      ?.getAttribute("d");

  it("holds each value until the next, in steps", () => {
    expect(edge([0, 10])).toBe("M0,29H50V1H100");
  });

  it("goes straight from a value to the next, as a line", () => {
    expect(edge([0, 10], "line")).toBe("M0,29L100,1");
  });

  it("draws nothing of no values", () => {
    const { container } = render(<Sparkline values={[]} />);
    expect(container.querySelector("svg")).toBeNull();
  });
});
