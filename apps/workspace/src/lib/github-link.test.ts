import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { linkGitHub } from "./github-link";

const fake = vi.hoisted(() => ({ linkGitHub: vi.fn(), githubMe: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));

beforeEach(() => fake.githubMe.mockResolvedValue(null));
afterEach(() => {
  vi.restoreAllMocks();
  vi.resetAllMocks();
  vi.useRealTimers();
});

it("is done at once when the server says the account is linked already", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  const open = vi.spyOn(window, "open");
  await linkGitHub();
  expect(open).not.toHaveBeenCalled();
});

it("opens GitHub in a popup and is done when the popup says so", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/login/oauth/authorize?state=s" });
  const open = vi.spyOn(window, "open").mockReturnValue({ closed: false } as Window);
  const done = linkGitHub();
  await vi.waitFor(() => expect(open).toHaveBeenCalledWith("https://github.test/login/oauth/authorize?state=s", "shpyrd-github-link", expect.stringContaining("popup")));
  window.dispatchEvent(new MessageEvent("message", { data: { github: "linked" }, origin: window.location.origin }));
  await expect(done).resolves.toBeUndefined();
});

it("ignores a message from another origin, and is done once the server shows the link", async () => {
  vi.useFakeTimers();
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  vi.spyOn(window, "open").mockReturnValue({ closed: false } as Window);
  let settled = false;
  const done = linkGitHub().then(() => (settled = true));
  await vi.advanceTimersByTimeAsync(0);
  window.dispatchEvent(new MessageEvent("message", { data: { github: "linked" }, origin: "https://evil.test" }));
  await vi.advanceTimersByTimeAsync(2_000);
  expect(settled).toBe(false);
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  await vi.advanceTimersByTimeAsync(2_000);
  await done;
  expect(settled).toBe(true);
});
