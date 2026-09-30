import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Card, CardContent, CardTitle } from "./card";

describe("Card", () => {
  it("is the default one when nothing is said", () => {
    render(<Card data-testid="card" />);
    expect(screen.getByTestId("card").getAttribute("data-variant")).toBe("default");
    expect(screen.getByTestId("card").tagName).toBe("DIV");
  });

  it("says which variant it is, for the shadow of the secondary", () => {
    render(<Card data-testid="card" variant="secondary" />);
    expect(screen.getByTestId("card").getAttribute("data-variant")).toBe("secondary");
  });

  it("becomes a link, with everything in it inside the link", () => {
    render(
      <Card asChild variant="secondary">
        <a href="/projects/hello">
          <CardTitle>hello</CardTitle>
          <CardContent>Running</CardContent>
        </a>
      </Card>,
    );
    const link = screen.getByRole("link");
    expect(link.getAttribute("href")).toBe("/projects/hello");
    expect(link.getAttribute("data-slot")).toBe("card");
    expect(link.getAttribute("data-variant")).toBe("secondary");
    expect(link.textContent).toBe("helloRunning");
  });
});
