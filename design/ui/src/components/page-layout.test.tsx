import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  PageLayout,
  PageLayoutContent,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutPane,
  PageLayoutSidebar,
} from "./page-layout";

describe("PageLayout", () => {
  it("has the landmarks of a page, whatever the order its parts are written in", () => {
    render(
      <PageLayout>
        <PageLayoutFooter>footer</PageLayoutFooter>
        <PageLayoutContent>content</PageLayoutContent>
        <PageLayoutSidebar aria-label="Gallery">sidebar</PageLayoutSidebar>
        <PageLayoutHeader>header</PageLayoutHeader>
      </PageLayout>,
    );
    expect(screen.getByRole("banner").textContent).toBe("header");
    expect(screen.getByRole("main").textContent).toBe("content");
    expect(screen.getByRole("contentinfo").textContent).toBe("footer");
    expect(screen.getByRole("complementary", { name: "Gallery" }).textContent).toBe("sidebar");
  });

  it("gives each part its place by name", () => {
    const { container } = render(
      <PageLayout>
        <PageLayoutHeader />
        <PageLayoutContent />
        <PageLayoutPane position="start" />
        <PageLayoutSidebar position="end" />
        <PageLayoutFooter />
      </PageLayout>,
    );
    const place = (slot: string) =>
      /\[grid-area:([a-z-]+)\]/.exec(
        container.querySelector(`[data-slot=page-layout-${slot}]`)?.className ?? "",
      )?.[1];
    expect(["header", "content", "pane", "sidebar", "footer"].map(place)).toEqual([
      "header",
      "content",
      "pane-start",
      "sidebar-end",
      "footer",
    ]);
  });

  it("puts the pane at the end and the sidebar at the start when nothing is said", () => {
    const { container } = render(
      <PageLayout>
        <PageLayoutPane />
        <PageLayoutSidebar />
      </PageLayout>,
    );
    expect(
      container.querySelector("[data-slot=page-layout-pane]")?.getAttribute("data-position"),
    ).toBe("end");
    expect(
      container.querySelector("[data-slot=page-layout-sidebar]")?.getAttribute("data-position"),
    ).toBe("start");
  });

  it("lets the content be another element, inside a page that has its main", () => {
    const { container } = render(
      <PageLayout>
        <PageLayoutContent as="div" />
      </PageLayout>,
    );
    expect(container.querySelector("[data-slot=page-layout-content]")?.tagName).toBe("DIV");
    expect(screen.queryByRole("main")).toBeNull();
  });

  it("hides a part always, or only when there is little room", () => {
    const { container } = render(
      <PageLayout>
        <PageLayoutHeader hidden />
        <PageLayoutSidebar hidden={{ narrow: true }} />
        <PageLayoutFooter hidden={{ regular: true }} />
      </PageLayout>,
    );
    const classes = (slot: string) =>
      (container.querySelector(`[data-slot=page-layout-${slot}]`)?.className ?? "").split(" ");
    expect(classes("header")).toContain("hidden");
    expect(classes("sidebar")).toContain("@max-3xl/page-layout:hidden");
    expect(classes("footer")).toContain("@3xl/page-layout:hidden");
  });

  it("makes room over a pane that stays in sight, for a header that does too", () => {
    const { container } = render(
      <PageLayout>
        <PageLayoutPane sticky offsetHeader={64} />
      </PageLayout>,
    );
    const pane = container.querySelector<HTMLElement>("[data-slot=page-layout-pane]");
    expect(pane?.style.getPropertyValue("--offset-header")).toBe("64px");
  });
});
