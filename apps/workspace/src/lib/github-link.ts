import { api } from "@/api/api";

// The code of the refusal that asks the person to link GitHub first.
export const LINK_REQUIRED = "github_link_required";

const every = 2_000;
const wait = 10 * 60_000;
const features = "popup,width=720,height=800";

// What a wait fails with when a newer one, or the screen leaving, ended
// it: nothing to tell the person.
export class LinkCancelled extends Error {
  constructor() {
    super("GitHub linking was cancelled.");
    this.name = "LinkCancelled";
  }
}

let current: (() => void) | undefined;

// Ends the wait in progress, if any, and closes its popup.
export function cancelGitHubLink() {
  current?.();
}

// Links the person's GitHub account in a popup, so the page under it keeps
// what was typed (RFC-0054). The popup opens at once, on the click, and
// goes to GitHub when the server answers. Done when the popup says so, or
// when the server shows the link (GitHub's pages may cut the popup from its
// opener), never because the popup reads as closed. One wait at a
// time: a new call, or cancelGitHubLink, ends the one before. The Mock
// links at once: an empty authorizeURL.
export function linkGitHub(): Promise<void> {
  cancelGitHubLink();
  const popup = window.open("", "shpyrd-github", features);
  return new Promise<void>((resolve, reject) => {
    let timer: ReturnType<typeof setInterval> | undefined;
    let ended = false;
    const end = (err?: Error) => {
      if (ended) return;
      ended = true;
      window.removeEventListener("message", onMessage);
      clearInterval(timer);
      if (current === cancel) current = undefined;
      if (err) reject(err);
      else resolve();
    };
    const cancel = () => {
      popup?.close();
      end(new LinkCancelled());
    };
    const onMessage = (e: MessageEvent) => {
      if (popup && e.source === popup && e.origin === window.location.origin && e.data?.github === "linked") end();
    };
    current = cancel;
    window.addEventListener("message", onMessage);
    api.linkGitHub(window.location.pathname + window.location.search).then(
      ({ authorizeURL }) => {
        if (ended) return;
        if (!authorizeURL) {
          popup?.close();
          return end();
        }
        if (!popup) {
          // Popups blocked: the page itself goes, and comes back when linked.
          window.location.assign(authorizeURL);
          return;
        }
        popup.location.href = authorizeURL;
        const started = Date.now();
        timer = setInterval(async () => {
          if (Date.now() - started > wait) return end(new Error("GitHub was not linked within 10 minutes. Try again."));
          // popup.closed says nothing: GitHub's Cross-Origin-Opener-Policy
          // makes a live popup read as closed. Only the link, the person
          // or the time ends the wait.
          try {
            if (await api.githubMe()) return end();
          } catch {
            // asked again at the next tick
          }
        }, every);
      },
      (err) => {
        popup?.close();
        end(err);
      },
    );
  });
}
