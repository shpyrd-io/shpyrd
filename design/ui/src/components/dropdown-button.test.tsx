import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DropdownButton } from "./dropdown-button";
import { DropdownMenuItem } from "./dropdown-menu";

const items = (
  <>
    <DropdownMenuItem>Restart</DropdownMenuItem>
    <DropdownMenuItem>See the logs</DropdownMenuItem>
  </>
);

// The menu opens on a key, as it does for who uses the keyboard.
const open = (button: HTMLElement) => {
  button.focus();
  fireEvent.keyDown(button, { key: "Enter" });
};

describe("DropdownButton", () => {
  it("is one button that opens the menu", () => {
    render(<DropdownButton label="Actions">{items}</DropdownButton>);
    const button = screen.getByRole("button", { name: "Actions" });
    expect(button.getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.queryByRole("menu")).toBeNull();
    open(button);
    expect(screen.getAllByRole("menuitem").map((i) => i.textContent)).toEqual([
      "Restart",
      "See the logs",
    ]);
  });

  it("opens nothing when it is disabled", () => {
    render(
      <DropdownButton label="Actions" disabled>
        {items}
      </DropdownButton>,
    );
    open(screen.getByRole("button", { name: "Actions" }));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("is split in two with a default action: the text does it, the chevron opens the menu", () => {
    const deploy = vi.fn();
    render(
      <DropdownButton label="Deploy" onClick={deploy}>
        {items}
      </DropdownButton>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Deploy" }));
    expect(deploy).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();

    open(screen.getByRole("button", { name: "More options" }));
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(deploy).toHaveBeenCalledTimes(1);
  });

  it("disables both parts together", () => {
    render(
      <DropdownButton label="Deploy" disabled onClick={() => {}} menuLabel="Other ways">
        {items}
      </DropdownButton>,
    );
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Deploy" }).disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Other ways" }).disabled).toBe(
      true,
    );
  });
});
