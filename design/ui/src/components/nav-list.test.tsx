import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  NavList,
  NavListAction,
  NavListDivider,
  NavListGroup,
  NavListItem,
  NavListSubNav,
} from "./nav-list";

describe("NavList", () => {
  it("is a navigation named by its heading, with the titles of its groups one level under", () => {
    render(
      <NavList heading="Settings">
        <NavListGroup title="Account">
          <NavListItem href="/profile">Profile</NavListItem>
        </NavListGroup>
      </NavList>,
    );
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 2, name: "Settings" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 3, name: "Account" })).toBeTruthy();
    expect(screen.getByRole("list", { name: "Account" })).toBeTruthy();
  });

  it("puts the titles of the groups a level deeper when its heading is", () => {
    render(
      <NavList heading="Settings" headingLevel="h3">
        <NavListGroup title="Account">
          <NavListItem href="/profile">Profile</NavListItem>
        </NavListGroup>
      </NavList>,
    );
    expect(screen.getByRole("heading", { level: 4, name: "Account" })).toBeTruthy();
  });

  it("keeps a heading that is not drawn for who cannot see", () => {
    render(
      <NavList heading="Settings" headingHidden>
        <NavListItem href="/profile">Profile</NavListItem>
      </NavList>,
    );
    expect(screen.getByRole("heading", { name: "Settings" }).className).toContain("sr-only");
    expect(screen.getByRole("navigation", { name: "Settings" })).toBeTruthy();
  });

  it("marks the item of the current page, and no other", () => {
    render(
      <NavList aria-label="Settings">
        <NavListItem href="/profile" aria-current="page">
          Profile
        </NavListItem>
        <NavListItem href="/appearance">Appearance</NavListItem>
      </NavList>,
    );
    const current = screen.getByRole("link", { name: "Profile" });
    const other = screen.getByRole("link", { name: "Appearance" });
    expect(current.getAttribute("aria-current")).toBe("page");
    expect(current.getAttribute("data-current")).toBe("true");
    expect(other.hasAttribute("aria-current")).toBe(false);
    expect(other.getAttribute("data-current")).toBe("false");
  });

  it("opens and closes the items under an item, which is a button and not a link", () => {
    render(
      <NavList aria-label="Project">
        <NavListSubNav title="Processes">
          <NavListItem href="/web">web</NavListItem>
        </NavListSubNav>
      </NavList>,
    );
    const toggle = screen.getByRole("button", { name: "Processes" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("link", { name: "web" })).toBeNull();

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("link", { name: "web" })).toBeTruthy();
    expect(document.getElementById(toggle.getAttribute("aria-controls") ?? "")?.hidden).toBe(false);
  });

  it("does what is beside an item without going where the item goes", () => {
    const pin = vi.fn();
    const go = vi.fn((event: React.MouseEvent) => event.preventDefault());
    render(
      <NavList aria-label="Projects">
        <NavListItem
          href="/hello"
          onClick={go}
          action={<NavListAction label="Pin hello" icon={<svg />} onClick={pin} />}
        >
          hello
        </NavListItem>
      </NavList>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Pin hello" }));
    expect(pin).toHaveBeenCalledTimes(1);
    expect(go).not.toHaveBeenCalled();
    expect(screen.getByRole("link", { name: "hello" }).contains(
      screen.getByRole("button", { name: "Pin hello" }),
    )).toBe(false);
  });

  it("becomes the link of a router, with its icon and what comes after", () => {
    render(
      <NavList aria-label="Project">
        <NavListItem asChild aria-current="page" icon={<svg data-testid="icon" />} iconEnd="2">
          <a href="/web" data-router="yes">
            web
          </a>
        </NavListItem>
      </NavList>,
    );
    const link = screen.getByRole("link");
    expect(link.getAttribute("data-router")).toBe("yes");
    expect(link.getAttribute("aria-current")).toBe("page");
    expect(link.contains(screen.getByTestId("icon"))).toBe(true);
    expect(link.textContent).toBe("web2");
  });

  it("has a line between items that is not an item", () => {
    render(
      <NavList aria-label="Projects">
        <NavListItem href="/a">a</NavListItem>
        <NavListDivider />
        <NavListItem href="/b">b</NavListItem>
      </NavList>,
    );
    expect(screen.getByRole("separator")).toBeTruthy();
    expect(screen.getAllByRole("link")).toHaveLength(2);
  });
});
