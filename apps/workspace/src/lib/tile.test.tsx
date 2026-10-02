import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import type { ProjectSummary } from "@/api/types";
import { toneOf } from "./project";
import { lookOf, ProjectTile } from "./tile";

const summary = (over: Partial<ProjectSummary>): ProjectSummary => ({ slug: "shop", displayName: "Shop", namespace: "p-shop", phase: "Running", release: 1, createdAt: "", access: "public", ...over });

describe("how a project is drawn on its card", () => {
  it("draws the symbol it chose in its colour", () => {
    const { container } = render(<ProjectTile project={summary({ icon: "truck", iconColor: "amber" })} />);
    expect(container.querySelector("[data-slot=app-icon]")?.getAttribute("class")).toContain("lucide-truck");
    expect(container.querySelector<HTMLElement>("span")?.style.getPropertyValue("--ink")).toBe("var(--symbol-amber)");
  });

  it("draws an SVG of its own as a symbol, and a PNG as a picture", () => {
    const svg = render(<ProjectTile project={summary({ icon: "truck", iconUrl: "/api/projects/shop/icon?v=1", iconType: "image/svg+xml" })} />);
    expect(svg.container.querySelector("[data-slot=app-symbol]")).not.toBeNull();
    expect(lookOf(summary({ iconUrl: "/i.png", iconType: "image/png" }))).toMatchObject({ picture: "/i.png", icon: undefined });
  });

  it("draws a window in the ink of its slug when it chose nothing", () => {
    const look = lookOf(summary({}));
    expect(look.tone).toBe(toneOf("shop"));
    const { container } = render(<ProjectTile project={summary({})} />);
    expect(container.querySelector("svg")?.getAttribute("class")).toContain("lucide-app-window");
  });
});
