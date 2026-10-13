import { expect, it, vi } from "vitest";
import { changelogPosts, changelogDate } from "./changelog";
vi.mock("server-only", () => ({}));

it("discovers published entries with unique stable slugs in descending date order", async () => {
  const posts = await changelogPosts();
  expect(posts.length).toBeGreaterThan(13);
  expect(new Set(posts.map((post) => post.slug)).size).toBe(posts.length);
  expect(posts.map((post) => post.date)).toEqual(posts.map((post) => post.date).sort().reverse());
  for (const post of posts) {
    expect(post.title).toBeTruthy();
    expect(post.description).toBeTruthy();
    expect(typeof post.Content).toBe("function");
    expect(["fix", "feature", "improvement"]).toContain(post.type);
  }
  expect(posts.find((post) => post.slug === "data-protection-and-sleep-hotfixes")?.type).toBe("fix");
});
it("formats UTC dates without repeating the year in the archive", () => {
  expect(changelogDate("2026-10-10")).toBe("Oct 10, 2026");
  expect(changelogDate("2026-10-10", false)).toBe("Oct 10");
});
