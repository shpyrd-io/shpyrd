import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { InfoTable, InfoTableItem } from "./info-table";

describe("InfoTable", () => {
  it("is a list of names and their values, named by its title", () => {
    const { container } = render(
      <InfoTable title="Summary">
        <InfoTableItem label="Name">Hello World</InfoTableItem>
        <InfoTableItem label="Release">v12</InfoTableItem>
      </InfoTable>,
    );
    const list = container.querySelector("dl") as HTMLElement;
    expect(document.getElementById(list.getAttribute("aria-labelledby") ?? "")?.textContent).toBe(
      "Summary",
    );
    expect([...list.querySelectorAll("dt")].map((t) => t.textContent)).toEqual(["Name", "Release"]);
    expect([...list.querySelectorAll("dd")].map((d) => d.textContent)).toEqual([
      "Hello World",
      "v12",
    ]);
  });

  it("is a grid when nothing is said, and rows when it is told", () => {
    const { container } = render(
      <>
        <InfoTable>
          <InfoTableItem label="a">1</InfoTableItem>
        </InfoTable>
        <InfoTable layout="rows">
          <InfoTableItem label="b">2</InfoTableItem>
        </InfoTable>
      </>,
    );
    expect([...container.querySelectorAll("dl")].map((l) => l.getAttribute("data-layout"))).toEqual([
      "grid",
      "rows",
    ]);
  });

  it("writes a dash for a value that is not there", () => {
    render(
      <InfoTable>
        <InfoTableItem label="Address" />
        <InfoTableItem label="Note">{""}</InfoTableItem>
      </InfoTable>,
    );
    expect(screen.getAllByText("—")).toHaveLength(2);
  });

  it("keeps a plain value whole for who points at it", () => {
    render(
      <InfoTable>
        <InfoTableItem label="Address" truncate>
          hello-world.platform.shpyrd.app
        </InfoTableItem>
      </InfoTable>,
    );
    expect(screen.getByTitle("hello-world.platform.shpyrd.app").className.split(" ")).toContain(
      "truncate",
    );
  });
});
