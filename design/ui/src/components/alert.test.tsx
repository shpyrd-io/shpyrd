import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Alert, AlertDescription, AlertTitle } from "./alert";

describe("Alert", () => {
  it("has an icon of its kind, and none by default", () => {
    const { container } = render(
      <>
        <Alert variant="info">
          <AlertTitle>Info</AlertTitle>
        </Alert>
        <Alert>
          <AlertTitle>Default</AlertTitle>
        </Alert>
      </>,
    );
    const alerts = container.querySelectorAll("[data-slot=alert]");
    expect(alerts[0].querySelector("[data-slot=alert-icon]")).not.toBeNull();
    expect(alerts[1].querySelector("[data-slot=alert-icon]")).toBeNull();
  });

  it("takes an icon of its own, or none", () => {
    const { container } = render(
      <>
        <Alert variant="info" icon={<svg data-testid="own" />}>
          <AlertTitle>Own</AlertTitle>
        </Alert>
        <Alert variant="info" icon={null}>
          <AlertTitle>None</AlertTitle>
        </Alert>
      </>,
    );
    expect(screen.getByTestId("own")).not.toBeNull();
    expect(container.querySelectorAll("[data-slot=alert-icon]")).toHaveLength(1);
  });

  it("interrupts only for what asks for care or went wrong", () => {
    render(
      <>
        <Alert variant="warning">
          <AlertTitle>Warning</AlertTitle>
        </Alert>
        <Alert variant="success">
          <AlertDescription>Done</AlertDescription>
        </Alert>
      </>,
    );
    expect(screen.getByRole("alert").textContent).toBe("Warning");
    expect(screen.getByRole("status").textContent).toBe("Done");
  });

  it("closes with a button when it may be dismissed", () => {
    const onDismiss = vi.fn();
    render(
      <Alert onDismiss={onDismiss}>
        <AlertTitle>Verified</AlertTitle>
      </Alert>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
