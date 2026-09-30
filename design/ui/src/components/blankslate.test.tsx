import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Blankslate } from "./blankslate";

describe("Blankslate", () => {
  it("needs only its title", () => {
    const { container } = render(<Blankslate title="No projects yet" />);
    expect(screen.getByText("No projects yet")).toBeTruthy();
    expect(container.querySelector("[data-slot=blankslate-graphic]")).toBeNull();
    expect(container.querySelector("[data-slot=blankslate-actions]")).toBeNull();
    expect(container.firstElementChild?.getAttribute("data-border")).toBe("false");
  });

  it("shows every part it is given, the action over the secondary one", () => {
    const { container } = render(
      <Blankslate
        border
        graphic={<svg data-testid="graphic" />}
        title="No projects yet"
        description="Make the first one."
        action={<button>New project</button>}
        secondaryAction={<a href="/docs">Read about projects</a>}
      />,
    );
    expect(container.firstElementChild?.getAttribute("data-border")).toBe("true");
    expect(screen.getByTestId("graphic")).toBeTruthy();
    expect(screen.getByText("Make the first one.")).toBeTruthy();
    const actions = container.querySelector("[data-slot=blankslate-actions]");
    expect([...(actions?.children ?? [])].map((c) => c.textContent)).toEqual([
      "New project",
      "Read about projects",
    ]);
  });
});
