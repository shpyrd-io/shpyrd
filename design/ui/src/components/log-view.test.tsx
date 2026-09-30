import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { atLeast, lineMatches, LogView, plain, TextLogView, type LogLine } from "./log-view";

const lines: LogLine[] = [
  { time: "17:04:01", instance: "web-1", level: "info", levelText: "info", message: "listening on :3000" },
  { time: "17:04:03", instance: "worker-1", message: "booting worker" },
  {
    time: "17:04:15",
    instance: "worker-1",
    level: "error",
    levelText: "error",
    message: "job failed",
    fields: [
      { key: "job", value: "reports.monthly" },
      { key: "error", value: '{"name":"TimeoutError"}', json: { name: "TimeoutError" } },
    ],
  },
  { time: "17:04:16", instance: "web-2", level: "error", message: "Error: connection reset" },
  { time: "17:04:40", instance: "web-1", message: "\u001b[32mGET\u001b[0m /healthz" },
];

describe("LogView", () => {
  it("writes when, from where and what", () => {
    render(<LogView lines={lines.slice(0, 1)} />);
    expect(screen.getByText("17:04:01")).not.toBeNull();
    expect(screen.getByText("web-1")).not.toBeNull();
    expect(screen.getByText("listening on :3000")).not.toBeNull();
  });

  it("shows a level that was declared, and dims one read off the text", () => {
    const { container } = render(<LogView lines={[lines[2], lines[3]]} />);
    const levels = container.querySelectorAll("[data-level=error] .uppercase");
    expect(levels).toHaveLength(2);
    expect(levels[0].className).not.toContain("opacity-60");
    expect(levels[1].className).toContain("opacity-60");
  });

  it("gives a plain line no level", () => {
    const { container } = render(<LogView lines={[lines[1]]} />);
    expect(container.querySelector(".uppercase")).toBeNull();
  });

  it("opens the fields of a record, and the objects within them", () => {
    render(<LogView lines={[lines[2]]} />);
    expect(screen.queryByText("reports.monthly")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show fields" }));
    expect(screen.getByText("reports.monthly")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show error" }));
    expect(screen.getByText("TimeoutError")).not.toBeNull();
  });

  it("keeps only the lines of the level asked, and of the words asked", () => {
    const { rerender } = render(<LogView lines={lines} level="error" />);
    expect(screen.queryByText("listening on :3000")).toBeNull();
    expect(screen.getByText("job failed")).not.toBeNull();
    rerender(<LogView lines={lines} filter="monthly" />);
    expect(screen.getByText("job failed")).not.toBeNull();
    expect(screen.queryByText("booting worker")).toBeNull();
    rerender(<LogView lines={lines} filter="nothing of the sort" />);
    expect(screen.getByText("No line matches the filter")).not.toBeNull();
  });

  it("takes the colour codes out", () => {
    render(<LogView lines={[lines[4]]} />);
    expect(screen.getByText("GET /healthz")).not.toBeNull();
    expect(plain("\u001b[31mred\u001b[0m")).toBe("red");
  });

  it("says when there is nothing", () => {
    render(<LogView lines={[]} empty="Waiting" />);
    expect(screen.getByText("Waiting")).not.toBeNull();
  });
});

describe("atLeast and lineMatches", () => {
  it("ranks the levels, and reads a plain line as info", () => {
    expect(atLeast("error", "warn")).toBe(true);
    expect(atLeast("debug", "info")).toBe(false);
    expect(atLeast(undefined, "info")).toBe(true);
    expect(atLeast(undefined, "warn")).toBe(false);
  });

  it("looks in the message, the instance and the fields", () => {
    expect(lineMatches(lines[2], "WORKER")).toBe(true);
    expect(lineMatches(lines[2], "timeout")).toBe(true);
    expect(lineMatches(lines[0], "timeout")).toBe(false);
    expect(lineMatches(lines[0], "  ")).toBe(true);
  });
});

describe("TextLogView", () => {
  it("makes the steps stand out and the errors red", () => {
    render(<TextLogView lines={["===> Building", "added 312 packages", "Error: worker exited"]} />);
    expect(screen.getByText("===> Building").className).toContain("text-primary");
    expect(screen.getByText("Error: worker exited").className).toContain("text-destructive");
    expect(screen.getByText("added 312 packages").className).toBe("");
  });
});
