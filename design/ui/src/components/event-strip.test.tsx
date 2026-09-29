import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { EventStrip } from "./event-strip";

const rows = [
  {
    label: "Failed",
    tone: "error" as const,
    events: [
      { time: 25, count: 3 },
      { time: 75, count: 1 },
    ],
  },
  { label: "Release", tone: "info" as const, events: [{ time: 50, title: "v12 went out" }] },
  { label: "Scale", events: [] },
];

describe("EventStrip", () => {
  it("has a row for each kind of thing, with how many there were", () => {
    const { container } = render(<EventStrip rows={rows} from={0} to={100} />);
    const totals = [...container.querySelectorAll("ul li")].map((item) => item.textContent);
    expect(totals).toEqual(["Failed4", "Release1", "Scale0"]);
  });

  it("puts each mark at its moment", () => {
    const { container } = render(<EventStrip rows={rows} from={0} to={100} />);
    const marks = [...container.querySelectorAll<HTMLElement>(".relative.h-5 > span")];
    expect(marks.map((mark) => mark.style.left)).toEqual(["25%", "75%", "50%"]);
  });

  it("draws a moment stronger the more there was in it", () => {
    const { container } = render(<EventStrip rows={rows} from={0} to={100} />);
    const [three, one] = [...container.querySelectorAll<HTMLElement>(".relative.h-5 > span > span")];
    expect(Number(three.style.opacity)).toBeGreaterThan(Number(one.style.opacity));
  });

  it("tells what a mark is when it is pointed at", () => {
    render(<EventStrip rows={rows} from={0} to={100} />);
    expect(screen.getByTitle(/^v12 went out, /)).toBeTruthy();
    expect(screen.getByTitle(/^Failed, 3 times, /)).toBeTruthy();
  });

  it("gives each row the ink of what it means", () => {
    const { container } = render(<EventStrip rows={rows} from={0} to={100} />);
    const inks = [...container.querySelectorAll(".relative.h-5")].map(
      (row) => /--ink:var\(--chart-([a-z0-9]+)\)/.exec(row.className)?.[1],
    );
    expect(inks).toEqual(["error", "info", "5"]);
  });
});
