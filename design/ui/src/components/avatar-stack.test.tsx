import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { AvatarStack } from "./avatar-stack";

const people = ["Ana Souza", "Marcelo Lima", "Beatriz Costa", "Rafael Alves", "Carla Mendes", "Diego Ramos"].map(
  (alt) => ({ alt }),
);

describe("AvatarStack", () => {
  it("shows everyone when they fit the places", () => {
    render(<AvatarStack avatars={people.slice(0, 4)} />);
    expect(screen.getAllByRole("img")).toHaveLength(4);
    expect(screen.queryByText(/^\+/)).toBeNull();
  });

  it("keeps the last place for how many more there are", () => {
    render(<AvatarStack avatars={people} max={4} />);
    const images = screen.getAllByRole("img");
    expect(images).toHaveLength(4);
    const count = screen.getByRole("img", { name: "3 more" });
    expect(count.textContent).toBe("+3");
    expect(count.getAttribute("title")).toBe("Rafael Alves, Carla Mendes, Diego Ramos");
  });

  it("names each one for the pointer", () => {
    render(<AvatarStack avatars={people.slice(0, 2)} />);
    expect(screen.getByRole("img", { name: "Ana Souza" }).getAttribute("title")).toBe("Ana Souza");
  });

  it("says whether it spreads under the pointer", () => {
    const { container } = render(<AvatarStack avatars={people.slice(0, 2)} expand={false} />);
    expect(container.firstElementChild?.getAttribute("data-expand")).toBe("false");
  });
});
