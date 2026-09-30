import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Shipyard, shipyardSprites } from "./shipyard";

describe("Shipyard", () => {
  it("is a picture, with a word for who cannot see it", () => {
    render(<Shipyard paused />);
    expect(screen.getByRole("img").getAttribute("aria-label")).toMatch(/shipyard/i);
  });

  it("draws every piece from its file, put where it stands", () => {
    const { container } = render(<Shipyard paused />);
    const images = container.querySelectorAll("img");
    expect(images.length).toBeGreaterThan(20);
    for (const image of images) {
      expect(Object.values(shipyardSprites).some((sprite) => sprite.src === image.getAttribute("src"))).toBe(true);
      expect(image.style.transform).toMatch(/translate3d\(.+cqw, .+cqw, 0\)/);
    }
  });

  it("draws the same yard for the same seed, and another for another", () => {
    const one = render(<Shipyard paused seed={4} />).container.innerHTML;
    const same = render(<Shipyard paused seed={4} />).container.innerHTML;
    const other = render(<Shipyard paused seed={5} />).container.innerHTML;
    expect(same).toBe(one);
    expect(other).not.toBe(one);
  });
});
