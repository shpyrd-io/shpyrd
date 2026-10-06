import { describe, expect, it } from "vitest";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Card, CardContent } from "./card";
import { PageHeading } from "./page-heading";
import { InfoTable, InfoTableItem } from "./info-table";
import { Breadcrumbs, BreadcrumbsItem } from "./breadcrumbs";
import { NavList, NavListItem } from "./nav-list";
import { Tile } from "./tile";
import { DocumentActions } from "./document-actions";
import { MobileNavigation, MobileNavigationTrigger, MobileNavigationContent } from "./mobile-navigation";
import { PageLayoutSidebar } from "./page-layout";
import { SiteHeader } from "./site-header";

describe("shared design system extensions", () => {
  it("keeps background and application content inside a single interactive card link", () => {
    const html = renderToStaticMarkup(h(Card, { asChild: true, interactive: true, variant: "dashed", background: h("svg", { "aria-label": "art" }) }, h("a", { href: "/document" }, h(CardContent, null, "Terms"))));
    expect(html).toMatch(/^<a /);
    expect(html.match(/<a /g)).toHaveLength(1);
    expect(html).toContain('href="/document"');
    expect(html).toContain('data-slot="card-background" aria-hidden="true"');
    expect(html).toContain('data-variant="dashed"');
    expect(html).toContain('data-interactive="true"');
    expect(html).toContain("Terms");
  });
  it("keeps version notes separate from the heading and description", () => {
    const html = renderToStaticMarkup(h(PageHeading, { title: "Terms", notes: "Version 2", description: "Agreement" }));
    expect(html).toContain('data-slot="page-heading-notes"');
    expect(html).toMatch(/<h1[^>]*>Terms<\/h1>/);
    expect(html).toContain("Version 2");
  });
  it("retains definition-list semantics in a wrapping horizontal layout", () => {
    const html = renderToStaticMarkup(h(InfoTable, { layout: "horizontal" }, h(InfoTableItem, { label: "Version" }, "2")));
    expect(html).toContain('data-layout="horizontal"');
    expect(html).toContain("flex-wrap");
    expect(html).toMatch(/<dt[^>]*>Version<\/dt>/);
    expect(html).toMatch(/<dd[^>]*>2<\/dd>/);
  });
  it("uses decorative chevrons and preserves the current breadcrumb", () => {
    const html = renderToStaticMarkup(h(Breadcrumbs, null, h(BreadcrumbsItem, { href: "/" }, "Home"), h(BreadcrumbsItem, { selected: true }, "Terms")));
    expect(html).toContain("lucide-chevron-right");
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('aria-current="page"');
  });
  it("preserves active navigation in compact lists", () => {
    const html = renderToStaticMarkup(h(NavList, { size: "sm" }, h(NavListItem, { href: "/", "aria-current": "page" }, "Overview")));
    expect(html).toContain('data-size="sm"');
    expect(html).toContain('data-current="true"');
  });
  it("renders tiles without inventing interactive semantics", () => {
    const html = renderToStaticMarkup(h(Tile, { size: "sm", variant: "muted" }, "T"));
    expect(html).toMatch(/^<span /);
    expect(html).toContain('data-size="sm"');
    expect(html).not.toContain('role="button"');
  });
  it("wires mobile triggers to inline content and hides it by default", () => {
    const html = renderToStaticMarkup(h(MobileNavigation, null, h(MobileNavigationTrigger), h(MobileNavigationContent, null, "Navigation")));
    const target = html.match(/aria-controls="([^"]+)"/)?.[1];
    expect(target).toBeTruthy();
    expect(html).toContain(`id="${target}" hidden=""`);
    expect(html).toContain('aria-expanded="false"');
  });
  it("uses inline mobile navigation in the original SiteHeader", () => {
    const html = renderToStaticMarkup(h(SiteHeader, { links: [h("a", { href: "/docs", key: "docs", "aria-current": "page" as const }, "Docs")] }));
    expect(html).toContain('data-slot="mobile-navigation"');
    expect(html).toContain('aria-expanded="false"');
  });
  it("accepts application labels and an actual download URL", () => {
    const html = renderToStaticMarkup(h(DocumentActions, { permalink: "/v2", downloadHref: "/api/v2", labels: { copy: "Copiar", copied: "Copiado", copyFailed: "Link", download: "Baixar", print: "Imprimir" } }));
    expect(html).toContain("Copiar");
    expect(html).toContain('href="/api/v2" download=""');
    expect(html).toContain('role="status"');
  });
});


describe("sidebar footer composition", () => {
  it("keeps the footer outside the scrollable content without changing the default width or surface", () => {
    const html = renderToStaticMarkup(h(PageLayoutSidebar, { width: "small", sticky: true, footer: h("a", { href: "/" }, "Back") }, h("nav", null, "Documents")));
    expect(html).toContain('data-variant="fixed-footer"');
    expect(html).toMatch(/data-slot="page-layout-sidebar-content"[^>]*><nav>Documents<\/nav><\/div><div data-slot="page-layout-sidebar-footer"/);
    expect(html).toContain("@3xl/page-layout:w-60");
    expect(html).not.toContain("bg-sidebar");
    const plain = renderToStaticMarkup(h(PageLayoutSidebar, null, "Documents"));
    expect(plain).not.toContain('data-slot="page-layout-sidebar-content"');
    expect(plain).not.toContain('data-slot="page-layout-sidebar-footer"');
  });
});
