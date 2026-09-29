import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AnchoredOverlay, AnchoredOverlayClose } from "./anchored-overlay";

const overlay = () => document.querySelector<HTMLElement>("[data-slot=anchored-overlay-content]");

describe("AnchoredOverlay", () => {
  it("is closed until its anchor is pressed", () => {
    render(<AnchoredOverlay anchor={<button>Toggle</button>}>Content</AnchoredOverlay>);
    expect(overlay()).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }));
    expect(overlay()?.textContent).toBe("Content");
    expect(screen.getByRole("button", { name: "Toggle" }).getAttribute("aria-expanded")).toBe(
      "true",
    );
  });

  it("closes with its anchor, with Escape and with a part made to close it", () => {
    render(
      <AnchoredOverlay anchor={<button>Toggle</button>}>
        <AnchoredOverlayClose asChild>
          <button>Close</button>
        </AnchoredOverlayClose>
      </AnchoredOverlay>,
    );
    const anchor = screen.getByRole("button", { name: "Toggle" });

    fireEvent.click(anchor);
    fireEvent.click(anchor);
    expect(overlay()).toBeNull();

    fireEvent.click(anchor);
    fireEvent.keyDown(overlay() as HTMLElement, { key: "Escape" });
    expect(overlay()).toBeNull();

    fireEvent.click(anchor);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(overlay()).toBeNull();
  });

  it("is placed under its anchor, from its start, when nothing is said", () => {
    render(
      <AnchoredOverlay anchor={<button>Toggle</button>} defaultOpen>
        Content
      </AnchoredOverlay>,
    );
    expect(overlay()?.getAttribute("data-side")).toBe("bottom");
    expect(overlay()?.getAttribute("data-align")).toBe("start");
  });

  it("takes a width and a height by name", () => {
    render(
      <AnchoredOverlay anchor={<button>Toggle</button>} defaultOpen width="small" height="xsmall">
        Content
      </AnchoredOverlay>,
    );
    const classes = overlay()?.className.split(" ") ?? [];
    expect(classes).toContain("w-64");
    expect(classes).toContain("h-48");
  });

  it("lets the page open and close it, and tells the page of every change", () => {
    const change = vi.fn();
    const { rerender } = render(
      <AnchoredOverlay anchor={<button>Toggle</button>} open={false} onOpenChange={change}>
        Content
      </AnchoredOverlay>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Toggle" }));
    expect(change).toHaveBeenCalledWith(true);
    expect(overlay()).toBeNull();

    rerender(
      <AnchoredOverlay anchor={<button>Toggle</button>} open onOpenChange={change}>
        Content
      </AnchoredOverlay>,
    );
    expect(overlay()).toBeTruthy();
  });
});
