import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AnnouncementBar } from "./announcement-bar";

describe("AnnouncementBar", () => {
  it("is dark whatever the theme of the page", () => {
    const { container } = render(<AnnouncementBar>News</AnnouncementBar>);
    expect(container.querySelector("[data-slot=announcement-bar]")!.classList).toContain("dark");
  });

  it("holds a white or an orange ground when asked to", () => {
    const { container } = render(
      <>
        <AnnouncementBar variant="light">News</AnnouncementBar>
        <AnnouncementBar variant="orange">News</AnnouncementBar>
      </>,
    );
    const [light, orange] = container.querySelectorAll("[data-slot=announcement-bar]");
    expect(light.classList).toContain("light");
    expect(light.classList).not.toContain("dark");
    expect(orange.classList).toContain("bg-primary");
    expect(orange.getAttribute("data-variant")).toBe("orange");
  });

  it("is a region named for what it is", () => {
    render(<AnnouncementBar>News</AnnouncementBar>);
    expect(screen.getByRole("region", { name: "Announcement" })).not.toBeNull();
  });

  it("draws its icon and its action only when given", () => {
    const { container, rerender } = render(<AnnouncementBar>News</AnnouncementBar>);
    expect(container.querySelector("[data-slot=announcement-bar-icon]")).toBeNull();
    expect(container.querySelector("[data-slot=announcement-bar-action]")).toBeNull();
    rerender(
      <AnnouncementBar icon={<svg data-testid="mark" />} action={<a href="/go">Go</a>}>
        News
      </AnnouncementBar>,
    );
    expect(screen.getByTestId("mark")).not.toBeNull();
    expect(screen.getByRole("link", { name: "Go" }).getAttribute("href")).toBe("/go");
  });

  it("closes only when asked to, by its own button", () => {
    const onDismiss = vi.fn();
    const { rerender } = render(<AnnouncementBar>News</AnnouncementBar>);
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
    rerender(<AnnouncementBar onDismiss={onDismiss}>News</AnnouncementBar>);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
