import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Button } from "./button";

const Icon = (props: React.ComponentProps<"svg">) => <svg data-testid="icon" {...props} />;

describe("Button", () => {
  it("puts the icon before the text, and marks where it is", () => {
    render(<Button icon={<Icon />}>New project</Button>);
    const button = screen.getByRole("button", { name: "New project" });
    expect(button.firstElementChild).toBe(screen.getByTestId("icon"));
    expect(screen.getByTestId("icon").getAttribute("data-icon")).toBe("inline-start");
  });

  it("puts the icon of the end after the text", () => {
    render(<Button iconEnd={<Icon />}>Continue</Button>);
    const button = screen.getByRole("button", { name: "Continue" });
    expect(button.lastElementChild).toBe(screen.getByTestId("icon"));
    expect(screen.getByTestId("icon").getAttribute("data-icon")).toBe("inline-end");
  });

  it("may be only an icon, with a name for who cannot see it", () => {
    render(<Button size="icon" icon={<Icon />} aria-label="Settings" />);
    expect(screen.getByRole("button", { name: "Settings" }).textContent).toBe("");
  });

  it("becomes its child and keeps its icon", () => {
    render(
      <Button asChild icon={<Icon />}>
        <a href="/projects">Projects</a>
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Projects" });
    expect(link.getAttribute("href")).toBe("/projects");
    expect(link.getAttribute("data-slot")).toBe("button");
    expect(link.firstElementChild).toBe(screen.getByTestId("icon"));
  });

  it("has a look for the bar of a site: the text alone, that turns to the brand's colour under the pointer", () => {
    render(<Button variant="nav">Docs</Button>);
    const button = screen.getByRole("button", { name: "Docs" });
    expect(button.className).toContain("text-foreground");
    expect(button.className).toContain("hover:text-primary");
    expect(button.className).toContain("aria-[current=page]:text-primary");
    expect(button.className).toContain("antialiased");
    expect(button.className).not.toMatch(/(^|\s|:)bg-(?!clip)/);
  });
});
