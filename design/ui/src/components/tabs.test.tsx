import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs";

const tabs = (variant?: "default" | "line") =>
  render(
    <Tabs defaultValue="overview">
      <TabsList variant={variant}>
        <TabsTrigger value="overview">Overview</TabsTrigger>
        <TabsTrigger value="releases" icon={<svg data-testid="icon" />} counter={12}>
          Releases
        </TabsTrigger>
        <TabsTrigger value="settings" disabled>
          Settings
        </TabsTrigger>
      </TabsList>
      <TabsContent value="overview">The panel of overview.</TabsContent>
      <TabsContent value="releases">The panel of releases.</TabsContent>
      <TabsContent value="settings">The panel of settings.</TabsContent>
    </Tabs>,
  );

describe("Tabs", () => {
  it("shows the panel of the tab that is open, and of no other", () => {
    tabs();
    expect(screen.getByRole("tabpanel").textContent).toBe("The panel of overview.");
    fireEvent.mouseDown(screen.getByRole("tab", { name: /Releases/ }), { button: 0 });
    expect(screen.getByRole("tabpanel").textContent).toBe("The panel of releases.");
    expect(screen.getByRole("tab", { name: /Releases/ }).getAttribute("aria-selected")).toBe(
      "true",
    );
  });

  it("does not open a tab that is disabled", () => {
    tabs();
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Settings" }), { button: 0 });
    expect(screen.getByRole("tabpanel").textContent).toBe("The panel of overview.");
  });

  it("says which variant its list is, the default one when nothing is said", () => {
    const { unmount } = tabs();
    expect(screen.getByRole("tablist").getAttribute("data-variant")).toBe("default");
    unmount();
    tabs("line");
    expect(screen.getByRole("tablist").getAttribute("data-variant")).toBe("line");
  });

  it("puts the icon before the text and the counter after it", () => {
    tabs("line");
    const tab = screen.getByRole("tab", { name: /Releases/ });
    expect(tab.firstElementChild).toBe(screen.getByTestId("icon"));
    expect(tab.lastElementChild?.getAttribute("data-slot")).toBe("tabs-counter");
    expect(tab.textContent).toBe("Releases12");
  });

  it("goes along a line, and only so", () => {
    render(
      // @ts-expect-error a list down the side is a NavList
      <Tabs defaultValue="a" orientation="vertical">
        <TabsList>
          <TabsTrigger value="a">a</TabsTrigger>
        </TabsList>
      </Tabs>,
    );
    expect(screen.getByRole("tablist").getAttribute("aria-orientation")).toBe("horizontal");
    expect(screen.getByRole("tablist").className).not.toContain("vertical");
  });
});
