import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Timeline, TimelineBreak, TimelineItem } from "./timeline";

describe("Timeline", () => {
  it("is a list in the order things happened", () => {
    render(
      <Timeline>
        <TimelineItem>built</TimelineItem>
        <TimelineItem>went out</TimelineItem>
      </Timeline>,
    );
    expect(screen.getByRole("list").tagName).toBe("OL");
    expect(screen.getAllByRole("listitem").map((i) => i.textContent)).toEqual([
      "built",
      "went out",
    ]);
  });

  it("says where its line is clipped, at both ends when only told to clip", () => {
    const { container } = render(
      <>
        <Timeline />
        <Timeline clip />
        <Timeline clip="start" />
      </>,
    );
    expect([...container.querySelectorAll("ol")].map((l) => l.getAttribute("data-clip"))).toEqual([
      null,
      "both",
      "start",
    ]);
  });

  it("gives the badge its type, and hides its icon from who cannot see it", () => {
    const { container } = render(
      <Timeline>
        <TimelineItem type="success" icon={<svg data-testid="icon" />}>
          went out
        </TimelineItem>
      </Timeline>,
    );
    expect(container.querySelector("[data-slot=timeline-badge]")?.getAttribute("data-type")).toBe(
      "success",
    );
    expect(screen.getByTestId("icon").getAttribute("aria-hidden")).toBe("true");
  });

  it("takes the colour of a badge from a name, never from the palette", () => {
    const { container } = render(
      <Timeline>
        <TimelineItem type="warning">restarted</TimelineItem>
      </Timeline>,
    );
    const classes = container.querySelector("[data-slot=timeline-badge]")?.className ?? "";
    expect(classes).toContain("bg-warning");
    expect(classes).not.toMatch(/(amber|emerald|sky|red)-\d/);
  });

  it("puts what can be done with an item at its end", () => {
    const { container } = render(
      <Timeline>
        <TimelineItem actions={<button>Go back to it</button>}>v11</TimelineItem>
      </Timeline>,
    );
    expect(container.querySelector("li")?.lastElementChild?.textContent).toBe("Go back to it");
  });

  it("has a break that says nothing to who cannot see it", () => {
    const { container } = render(
      <Timeline>
        <TimelineItem>one</TimelineItem>
        <TimelineBreak />
        <TimelineItem>two</TimelineItem>
      </Timeline>,
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(
      container.querySelector("[data-slot=timeline-break]")?.getAttribute("aria-hidden"),
    ).toBe("true");
  });
});
