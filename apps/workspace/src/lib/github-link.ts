import { api } from "@/api/api";

// The code of the refusal that asks the person to link GitHub first.
export const LINK_REQUIRED = "github_link_required";

const every = 2_000;
const wait = 10 * 60_000;

// Links the person's GitHub account in a popup, so the page under it keeps
// what was typed (RFC-0054). Done when the popup says so, or when the
// server shows the link (GitHub's pages may cut the popup from its opener).
// The Mock links at once: an empty authorizeURL.
export async function linkGitHub(): Promise<void> {
  const { authorizeURL } = await api.linkGitHub(window.location.pathname + window.location.search);
  if (!authorizeURL) return;
  const popup = window.open(authorizeURL, "shpyrd-github-link", "popup,width=720,height=800");
  if (!popup) {
    // Popups blocked: the page itself goes, and comes back when linked.
    window.location.assign(authorizeURL);
    return new Promise<void>(() => {});
  }
  return new Promise<void>((resolve, reject) => {
    const started = Date.now();
    const finish = (err?: Error) => {
      window.removeEventListener("message", onMessage);
      clearInterval(timer);
      if (err) reject(err);
      else resolve();
    };
    const onMessage = (e: MessageEvent) => {
      if (e.origin === window.location.origin && e.data?.github === "linked") finish();
    };
    const timer = setInterval(async () => {
      if (Date.now() - started > wait) return finish(new Error("GitHub was not linked within 10 minutes. Try again."));
      try {
        if (await api.githubMe()) finish();
      } catch {
        // asked again at the next tick
      }
    }, every);
    window.addEventListener("message", onMessage);
  });
}
