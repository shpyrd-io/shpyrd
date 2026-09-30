import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatePage } from "./state-page";

describe("StatePage", () => {
  it("says one thing, with the mark over it and the wordmark in the corner", () => {
    render(<StatePage title="Nothing here, yet." description="No application answers at this address." />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Nothing here, yet.");
    expect(screen.getByText("No application answers at this address.")).toBeTruthy();
    expect(screen.getByRole("img", { name: "shpyrd" })).toBeTruthy();
  });

  it("turns a spinner when something is on its way, and says nothing with no words", () => {
    const { rerender } = render(<StatePage title="Waking up." waiting />);
    expect(screen.getByLabelText("Waiting")).toBeTruthy();
    rerender(<StatePage />);
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.queryByLabelText("Waiting")).toBeNull();
  });
});
