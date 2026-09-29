import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { InlineCode } from "./inline-code";

describe("InlineCode", () => {
  it("is code, as big as the text around it", () => {
    render(<InlineCode>shpyrd deploy</InlineCode>);
    const code = screen.getByText("shpyrd deploy");
    expect(code.tagName).toBe("CODE");
    expect(code.className).toContain("text-[0.875em]");
  });

  it("may go to the next line, unless it is told to stay together", () => {
    render(
      <>
        <InlineCode>long</InlineCode>
        <InlineCode wrap={false}>/mcp</InlineCode>
      </>,
    );
    expect(screen.getByText("long").className.split(" ")).not.toContain("whitespace-nowrap");
    expect(screen.getByText("/mcp").className.split(" ")).toContain("whitespace-nowrap");
  });
});
