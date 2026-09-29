import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { TimeChart } from "./time-chart";

const series = [
  {
    name: "web",
    points: [
      [0, 10],
      [60, 20],
      [120, 30],
    ] as [number, number][],
  },
  {
    name: "worker",
    points: [
      [0, 1],
      [60, 2],
      [120, 3],
    ] as [number, number][],
  },
];

const legend = (container: HTMLElement) =>
  [...container.querySelectorAll("[data-slot=time-chart-legend] li")].map((item) =>
    item.textContent?.replace(/\s+/g, " ").trim(),
  );
const plot = (container: HTMLElement) =>
  container.querySelector("[data-slot=time-chart-plot]") as HTMLElement;

describe("TimeChart", () => {
  it("reads the last values in its legend", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    expect(legend(container)).toEqual(["web30", "worker3"]);
    expect(container.querySelector("[data-slot=time-chart-time]")?.textContent).toBe("latest");
  });

  it("reads the values of another moment when the arrows move along the time", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    fireEvent.keyDown(plot(container), { key: "ArrowLeft" });
    expect(legend(container)).toEqual(["web20", "worker2"]);
    fireEvent.keyDown(plot(container), { key: "Home" });
    expect(legend(container)).toEqual(["web10", "worker1"]);
    expect(container.querySelector("[data-slot=time-chart-crosshair]")).toBeTruthy();
  });

  it("goes back to the last values when it is left", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    fireEvent.keyDown(plot(container), { key: "Home" });
    fireEvent.keyDown(plot(container), { key: "Escape" });
    expect(legend(container)).toEqual(["web30", "worker3"]);
    expect(container.querySelector("[data-slot=time-chart-crosshair]")).toBeNull();
  });

  it("draws a wash and a line for each series", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    const groups = container.querySelectorAll("svg g[data-series]");
    expect([...groups].map((g) => g.getAttribute("data-series"))).toEqual(["web", "worker"]);
    for (const group of groups) expect(group.querySelectorAll("path")).toHaveLength(2);
  });

  it("gives each series an ink of its own, in the fixed order", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    const inks = [...container.querySelectorAll("svg g[data-series]")].map((g) => g.getAttribute("class"));
    expect(inks).toEqual(["[--ink:var(--chart-1)]", "[--ink:var(--chart-2)]"]);
  });

  it("gives every series the ink of the first, in shades", () => {
    const { container } = render(
      <TimeChart title="Response time" unit="ms" palette="shades" series={series} />,
    );
    const groups = [...container.querySelectorAll("svg g[data-series]")];
    expect(new Set(groups.map((g) => g.getAttribute("class"))).size).toBe(1);
    const washes = groups.map((g) => Number(g.querySelector("path")?.getAttribute("fill-opacity")));
    expect(washes[1]).toBeGreaterThan(washes[0]);
  });

  it("puts each series over the one before, in a stack", () => {
    const { container } = render(
      <TimeChart title="Throughput" unit="count" arrangement="stacked" series={series} />,
    );
    const edges = [...container.querySelectorAll("svg g[data-series] path[fill=none]")].map((p) =>
      p.getAttribute("d"),
    );
    // The top of the axis is 40: the stack reaches 33, which no series does.
    expect(container.querySelector(".relative.h-44")?.textContent).toContain("40");
    expect(edges[0]).not.toBe(edges[1]);
    expect(legend(container)).toEqual(["web30", "worker3"]);
  });

  it("makes room in its axis for what the series are measured against", () => {
    const { container } = render(
      <TimeChart
        title="Memory"
        unit="count"
        series={series}
        references={[{ value: 90, label: "Allocated" }]}
      />,
    );
    expect(screen.getByText("Allocated 90")).toBeTruthy();
    expect(container.querySelector(".relative.h-44")?.textContent).toContain("100");
  });

  it("shows what happened at a moment, under the time", () => {
    render(
      <TimeChart title="Instances" unit="count" series={series} markers={[{ time: 60, label: "v12" }]} />,
    );
    expect(screen.getByText("v12")).toBeTruthy();
  });

  it("has a table of its values, for who reads numbers and not drawings", () => {
    const { container } = render(<TimeChart title="Instances" unit="count" series={series} />);
    fireEvent.click(screen.getByRole("button", { name: "Show the values as a table" }));
    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Time",
      "web",
      "worker",
    ]);
    expect(screen.getAllByRole("row")).toHaveLength(4);
    expect(plot(container)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show the chart" }));
    expect(plot(container)).toBeTruthy();
  });

  it("says so when nothing was measured", () => {
    const { container } = render(<TimeChart title="Network" series={[]} />);
    expect(screen.getByText("Nothing was measured in this time")).toBeTruthy();
    expect(plot(container)).toBeNull();
  });
});
