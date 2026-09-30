import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { AppWindow } from "./app-window";

describe("AppWindow", () => {
  it("names the app in its title bar, with what it has open beside it", () => {
    render(
      <AppWindow title="Claude Code" detail="~/projects/crm">
        <p>The chat</p>
      </AppWindow>,
    );
    expect(screen.getByText("Claude Code")).toBeTruthy();
    expect(screen.getByText("~/projects/crm")).toBeTruthy();
    expect(screen.getByText("The chat")).toBeTruthy();
  });

  it("keeps what it is given at its foot under the content, and has no foot otherwise", () => {
    const { container, rerender } = render(
      <AppWindow title="Notes" footer={<input aria-label="Message" />}>
        content
      </AppWindow>,
    );
    expect(container.querySelector("[data-slot=app-window-footer]")).toBeTruthy();
    expect(screen.getByLabelText("Message")).toBeTruthy();
    rerender(<AppWindow title="Notes">content</AppWindow>);
    expect(container.querySelector("[data-slot=app-window-footer]")).toBeNull();
  });
});
