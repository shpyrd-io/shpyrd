import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { LauncherCard } from "./launcher-card";

describe("LauncherCard", () => {
  it("opens the application from anywhere on it, through its address", () => {
    render(<LauncherCard name="Corporate" url="corporate.acme.shpyrd.app" />);
    const link = screen.getByRole("link", { name: "corporate.acme.shpyrd.app" });
    expect(link.getAttribute("href")).toBe("https://corporate.acme.shpyrd.app");
    expect(link.className).toContain("after:absolute");
  });

  it("is a worker without an address: no link, its own icon gives way to the bot", () => {
    const { container } = render(
      <LauncherCard name="Reports" icon={<svg data-testid="own" />} phase="failed" />,
    );
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByTestId("own")).toBeNull();
    expect(container.querySelector("[data-slot=launcher-card]")?.getAttribute("data-worker")).toBe("true");
    expect(screen.getByText("Failed")).not.toBeNull();
  });

  it("says how it is with a light, and a moon when asleep", () => {
    const { container } = render(
      <>
        <LauncherCard name="A" url="a.example" phase="deploying" />
        <LauncherCard name="B" url="b.example" phase="sleeping" />
      </>,
    );
    const lights = container.querySelectorAll("[data-slot=launcher-semaphore]");
    expect(lights[0].getAttribute("aria-label")).toBe("Deploying");
    expect(lights[0].className).toContain("animate-pulse");
    expect(lights[1].getAttribute("data-phase")).toBe("sleeping");
    expect(lights[1].tagName).toBe("svg");
  });

  it("says where it answers and whether it is locked, with icons that have words", () => {
    render(<LauncherCard name="Mail" url="mail.example" exposure="internal" access="locked" />);
    expect(screen.getByRole("img", { name: "Local network" })).not.toBeNull();
    expect(screen.getByRole("img", { name: "Only for who signs in" })).not.toBeNull();
  });

  it("opens its settings from the gear, and has none without a handler", () => {
    const onSettings = vi.fn();
    const { rerender } = render(<LauncherCard name="Docs" url="docs.example" onSettings={onSettings} />);
    fireEvent.click(screen.getByRole("button", { name: "Settings of Docs" }));
    expect(onSettings).toHaveBeenCalledOnce();
    rerender(<LauncherCard name="Docs" url="docs.example" />);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("writes its tags", () => {
    render(<LauncherCard name="Docs" url="docs.example" tags={["Public", "Orders"]} />);
    expect(screen.getByText("Public")).not.toBeNull();
    expect(screen.getByText("Orders")).not.toBeNull();
  });

  it("is the same height as every other card, and cuts a long description at its second line", () => {
    const { container } = render(<LauncherCard name="Reports" url="reports.acme.app" description={"A long description ".repeat(20)} />);
    const card = container.querySelector("[data-slot=launcher-card]")!;
    expect(card.className).toContain("h-60");
    expect(card.className).toContain("overflow-hidden");
    expect(container.querySelector("p")?.className).toContain("line-clamp-2");
  });

  it("goes where it is told, when the door is not the address", () => {
    render(<LauncherCard name="Locked" url="locked.acme.app" href="https://locked.acme.app/.shpyrd/signin?rd=%2F" />);
    expect(screen.getByRole("link", { name: "locked.acme.app" }).getAttribute("href")).toBe("https://locked.acme.app/.shpyrd/signin?rd=%2F");
  });
});
