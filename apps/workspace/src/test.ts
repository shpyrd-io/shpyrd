// What the made up browser does not have and the library's components ask
// for: a slider measures its thumbs with a ResizeObserver.
class Observer {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= Observer as unknown as typeof ResizeObserver;
