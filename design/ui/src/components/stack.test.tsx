import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Stack, StackItem } from "./stack";

describe("Stack", () => {
  it("goes down the page, with the normal gap, when nothing is said", () => {
    render(<Stack data-testid="stack" />);
    const classes = screen.getByTestId("stack").className.split(" ");
    expect(classes).toContain("flex-col");
    expect(classes).toContain("gap-4");
    expect(classes).toContain("flex-nowrap");
  });

  it("goes along a line, wraps, aligns and justifies as it is told", () => {
    render(
      <Stack
        data-testid="stack"
        direction="horizontal"
        wrap="wrap"
        align="center"
        justify="space-between"
        gap="cozy"
        padding="normal"
      />,
    );
    const classes = screen.getByTestId("stack").className.split(" ");
    for (const name of ["flex-row", "flex-wrap", "items-center", "justify-between", "gap-3", "p-4"]) {
      expect(classes).toContain(name);
    }
  });

  it("has an item that takes the room that is left", () => {
    render(
      <Stack direction="horizontal">
        <StackItem grow data-testid="item" />
      </Stack>,
    );
    expect(screen.getByTestId("item").className.split(" ")).toContain("grow");
  });
});
