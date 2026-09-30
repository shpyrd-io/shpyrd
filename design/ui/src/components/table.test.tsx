import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "./table";

const rows = (
  <>
    <TableHeader>
      <TableRow>
        <TableHead>Project</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      <TableRow>
        <TableCell>Hello World</TableCell>
      </TableRow>
    </TableBody>
    <TableFooter>
      <TableRow>
        <TableCell>1</TableCell>
      </TableRow>
    </TableFooter>
  </>
);

describe("Table", () => {
  it("is the default one when nothing is said", () => {
    render(<Table>{rows}</Table>);
    expect(screen.getByRole("table").getAttribute("data-variant")).toBe("default");
  });

  it("says it is the secondary, which has lines only between the rows", () => {
    render(<Table variant="secondary">{rows}</Table>);
    const table = screen.getByRole("table");
    expect(table.getAttribute("data-variant")).toBe("secondary");
    expect(table.className).toContain("data-[variant=secondary]:[&_thead_tr]:border-b-0");
    expect(table.className).toContain("data-[variant=secondary]:[&_tfoot]:border-t-0");
  });

  it("keeps its header, its body and its footer", () => {
    render(<Table variant="secondary">{rows}</Table>);
    expect(screen.getByRole("columnheader").textContent).toBe("Project");
    expect(screen.getAllByRole("row")).toHaveLength(3);
  });
});
