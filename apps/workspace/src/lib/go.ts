// go sends the browser to another address (GitHub's install page, which
// sends it back). A module of its own so tests stand in for it.
export function go(url: string) {
  window.location.assign(url);
}
