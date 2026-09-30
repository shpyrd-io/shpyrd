import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatusBadge } from "./status-badge";

describe("StatusBadge", () => {
  it("says its type, and is neutral when it has none", () => {
    render(
      <>
        <StatusBadge type="success">Running</StatusBadge>
        <StatusBadge>Pending</StatusBadge>
      </>,
    );
    expect(screen.getByText("Running").getAttribute("data-type")).toBe("success");
    expect(screen.getByText("Pending").getAttribute("data-type")).toBe("neutral");
  });

  it("takes its colour from a name, never from the palette", () => {
    render(<StatusBadge type="warning">Deploying</StatusBadge>);
    const classes = screen.getByText("Deploying").className;
    expect(classes).toContain("text-warning");
    expect(classes).not.toMatch(/(amber|emerald|sky|red)-\d/);
  });

  it("writes the quantity after the word", () => {
    render(
      <StatusBadge variant="secondary" type="success" qty="1/1">
        web
      </StatusBadge>,
    );
    expect(screen.getByText("1/1").parentElement?.textContent).toBe("web1/1");
  });

  it("pulses its dot only while what it tells is going on", () => {
    const { container } = render(
      <>
        <StatusBadge live>Building</StatusBadge>
        <StatusBadge>Pending</StatusBadge>
      </>,
    );
    const dots = container.querySelectorAll("[aria-hidden]");
    expect(dots[0].className).toContain("animate-pulse");
    expect(dots[1].className).not.toContain("animate-pulse");
  });
});
