import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cancelGitHubLink, LinkCancelled, linkGitHub } from "./github-link";

const fake = vi.hoisted(() => ({ linkGitHub: vi.fn(), githubMe: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));

type Popup = { closed: boolean; close: ReturnType<typeof vi.fn>; location: { href: string } };
const newPopup = (): Popup => ({ closed: false, close: vi.fn(), location: { href: "" } });
// A message as the browser delivers it: from a window, with an origin.
function message(data: unknown, origin: string, source: unknown) {
  const e = new MessageEvent("message", { data, origin });
  Object.defineProperty(e, "source", { value: source });
  window.dispatchEvent(e);
}

beforeEach(() => fake.githubMe.mockResolvedValue(null));
afterEach(() => {
  cancelGitHubLink();
  vi.restoreAllMocks();
  vi.resetAllMocks();
  vi.useRealTimers();
});

it("opens the popup at once, before the server answers, then sends it to GitHub", async () => {
  const popup = newPopup();
  const open = vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
  let answer: (v: { authorizeURL: string }) => void = () => {};
  fake.linkGitHub.mockReturnValue(new Promise((r) => (answer = r)));
  const done = linkGitHub();
  expect(open).toHaveBeenCalledWith("", "shpyrd-github", expect.stringContaining("popup"));
  expect(popup.location.href).toBe("");
  answer({ authorizeURL: "https://github.test/login/oauth/authorize?state=s" });
  await vi.waitFor(() => expect(popup.location.href).toBe("https://github.test/login/oauth/authorize?state=s"));
  message({ github: "linked" }, window.location.origin, popup);
  await expect(done).resolves.toBeUndefined();
});

it("closes the popup and is done when the server says the account is linked already", async () => {
  const popup = newPopup();
  vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  await linkGitHub();
  expect(popup.close).toHaveBeenCalled();
});

it("closes the popup and fails when the server refuses", async () => {
  const popup = newPopup();
  vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
  fake.linkGitHub.mockRejectedValue(new Error("nope"));
  await expect(linkGitHub()).rejects.toThrow("nope");
  expect(popup.close).toHaveBeenCalled();
});

it("ignores a message from another origin, or from another window, and is done once the server shows the link", async () => {
  vi.useFakeTimers();
  const popup = newPopup();
  vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  let settled = false;
  const done = linkGitHub().then(() => (settled = true));
  await vi.advanceTimersByTimeAsync(0);
  message({ github: "linked" }, "https://evil.test", popup);
  message({ github: "linked" }, window.location.origin, newPopup());
  message({ github: "other" }, window.location.origin, popup);
  await vi.advanceTimersByTimeAsync(2_000);
  expect(settled).toBe(false);
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  await vi.advanceTimersByTimeAsync(2_000);
  await done;
  expect(settled).toBe(true);
});

it("keeps waiting when the popup reads as closed, and picks up a link made meanwhile", async () => {
  // GitHub's Cross-Origin-Opener-Policy makes a live popup read as closed.
  vi.useFakeTimers();
  const popup = newPopup();
  vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  let settled = false;
  const done = linkGitHub().then(() => (settled = true));
  await vi.advanceTimersByTimeAsync(2_000);
  popup.closed = true;
  await vi.advanceTimersByTimeAsync(6_000);
  expect(settled).toBe(false);
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  await vi.advanceTimersByTimeAsync(2_000);
  await done;
  expect(settled).toBe(true);
});

it("gives up after ten minutes without a link", async () => {
  vi.useFakeTimers();
  vi.spyOn(window, "open").mockReturnValue(newPopup() as unknown as Window);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  const failed = expect(linkGitHub()).rejects.toThrow(/not linked within 10 minutes/);
  await vi.advanceTimersByTimeAsync(10 * 60_000 + 4_000);
  await failed;
});

it("cancels the wait before it when called again, and when cancelled", async () => {
  vi.useFakeTimers();
  const first = newPopup();
  const second = newPopup();
  vi.spyOn(window, "open").mockReturnValueOnce(first as unknown as Window).mockReturnValueOnce(second as unknown as Window);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  const a = linkGitHub();
  const aCancelled = expect(a).rejects.toBeInstanceOf(LinkCancelled);
  await vi.advanceTimersByTimeAsync(0);
  const b = linkGitHub();
  await aCancelled;
  expect(first.close).toHaveBeenCalled();
  // The first wait's message no longer counts; the second's does.
  message({ github: "linked" }, window.location.origin, first);
  await vi.advanceTimersByTimeAsync(0);
  message({ github: "linked" }, window.location.origin, second);
  await expect(b).resolves.toBeUndefined();
  const c = linkGitHub();
  const cCancelled = expect(c).rejects.toBeInstanceOf(LinkCancelled);
  cancelGitHubLink();
  await cCancelled;
});
