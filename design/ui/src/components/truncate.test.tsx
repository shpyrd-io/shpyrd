import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Truncate } from "./truncate";

describe("Truncate", () => {
  it("is at most 125 pixels wide when nothing is said", () => {
    render(<Truncate>hello-world.platform.shpyrd.app</Truncate>);
    const text = screen.getByText("hello-world.platform.shpyrd.app");
    expect(text.style.getPropertyValue("--truncate-max")).toBe("125px");
  });

  it("takes a width in pixels or in what CSS takes", () => {
    render(
      <>
        <Truncate maxWidth={240}>one</Truncate>
        <Truncate maxWidth="10ch">two</Truncate>
      </>,
    );
    expect(screen.getByText("one").style.getPropertyValue("--truncate-max")).toBe("240px");
    expect(screen.getByText("two").style.getPropertyValue("--truncate-max")).toBe("10ch");
  });

  it("keeps the whole text for who points at it", () => {
    render(<Truncate>hello-world.platform.shpyrd.app</Truncate>);
    expect(screen.getByTitle("hello-world.platform.shpyrd.app")).toBeTruthy();
  });

  it("uses the title it is given, when what it holds is not plain text", () => {
    render(
      <Truncate title="The whole of it">
        <b>Part</b>
      </Truncate>,
    );
    expect(screen.getByTitle("The whole of it").textContent).toBe("Part");
  });

  it("sits along the line of a text, as a span", () => {
    render(
      <Truncate as="span" inline>
        7f3c9a1
      </Truncate>,
    );
    const text = screen.getByText("7f3c9a1");
    expect(text.tagName).toBe("SPAN");
    expect(text.className.split(" ")).toContain("inline-block");
  });
});
