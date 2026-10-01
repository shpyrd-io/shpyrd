import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "./navigation-menu";

function Menu() {
  return (
    <NavigationMenu aria-label="Site">
      <NavigationMenuList>
        <NavigationMenuItem>
          <NavigationMenuTrigger>Solutions</NavigationMenuTrigger>
          <NavigationMenuContent>
            <NavigationMenuLink href="/for/it" title="For IT teams" description="One accepted place." />
          </NavigationMenuContent>
        </NavigationMenuItem>
      </NavigationMenuList>
    </NavigationMenu>
  );
}

describe("NavigationMenu", () => {
  it("is a navigation whose words are closed until pressed", () => {
    render(<Menu />);
    expect(screen.getByRole("navigation", { name: "Site" })).toBeTruthy();
    const trigger = screen.getByRole("button", { name: "Solutions" });
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("For IT teams")).toBeNull();
  });

  it("opens a word's pages, each with its name and a line on what it is", () => {
    render(<Menu />);
    fireEvent.click(screen.getByRole("button", { name: "Solutions" }));
    expect(screen.getByRole("button", { name: "Solutions" }).getAttribute("aria-expanded")).toBe("true");
    const link = screen.getByRole("link", { name: /For IT teams/ });
    expect(link.getAttribute("href")).toBe("/for/it");
    expect(link.textContent).toContain("One accepted place.");
  });

  it("puts the name and the line inside a link it is given", () => {
    render(
      <NavigationMenu>
        <NavigationMenuList>
          <NavigationMenuItem>
            <NavigationMenuLink asChild title="Docs" description="Every command.">
              <a href="/docs">Docs</a>
            </NavigationMenuLink>
          </NavigationMenuItem>
        </NavigationMenuList>
      </NavigationMenu>,
    );
    const link = screen.getByRole("link", { name: /Docs/ });
    expect(link.getAttribute("href")).toBe("/docs");
    expect(link.textContent).toContain("Every command.");
  });

  it("opens its panel a little under the bar, not on its line", () => {
    const { container } = render(<Menu />);
    fireEvent.click(screen.getByRole("button", { name: "Solutions" }));
    expect(container.querySelector("[data-slot=navigation-menu-viewport]")?.className).toContain("mt-3");
  });
});
