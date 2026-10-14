import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { ChangelogArchive } from "./changelog-archive";
import { ChangelogSummary } from "./changelog";

it("reveals ten summaries at a time, preserves year groups and hides the exhausted action", () => {
  const entries = Array.from({ length: 21 }, (_, index) => ({
    slug: `entry-${index}`, year: index < 11 ? "2026" : "2025",
    content: <ChangelogSummary key={index} title={<a href={`/updates/${index}`}>Update {index}</a>} date="Oct 1" tag="fix">Summary {index}</ChangelogSummary>,
  }));
  render(<ChangelogArchive entries={entries} title="Earlier updates" loadMore="Load older updates" />);
  expect(screen.getAllByRole("link")).toHaveLength(10);
  expect(screen.queryByRole("heading", { name: "2025" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Load older updates" }));
  expect(screen.getAllByRole("link")).toHaveLength(20);
  expect(within(screen.getByRole("region", { name: "2026" })).getAllByRole("link")).toHaveLength(11);
  expect(within(screen.getByRole("region", { name: "2025" })).getAllByRole("link")).toHaveLength(9);
  fireEvent.click(screen.getByRole("button", { name: "Load older updates" }));
  expect(screen.getAllByRole("link")).toHaveLength(21);
  expect(screen.queryByRole("button")).toBeNull();
  expect(screen.getByRole("link", { name: "Update 20" }).getAttribute("href")).toBe("/updates/20");
});

it("omits an empty archive", () => {
  const { container } = render(<ChangelogArchive entries={[]} title="Earlier updates" loadMore="More" />);
  expect(container.innerHTML).toBe("");
});
