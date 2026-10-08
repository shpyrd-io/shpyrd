// The panel of frosted glass that the site's header and foot float as: held
// off the edges, the page passing blurred under it, lit along its top edge.
// On black it is lifted more: a lighter ground, a brighter edge and top light,
// or it is lost in the page.
export const glass =
  "rounded-2xl bg-background/75 ring-1 ring-foreground/8 backdrop-blur-xl backdrop-saturate-150 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_10px_30px_rgb(20_20_30/0.06)] dark:bg-accent/70 dark:ring-foreground/15 dark:shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_10px_30px_rgb(0_0_0/0.6)]";

// The same glass for a panel that holds another: its blur and ground on a
// layer behind it rather than on itself. A panel that blurs what is under it
// keeps what it holds from blurring anything outside it, so a menu that opens
// out of the header's bar would show the page sharp through its ground.
export const glassHolding =
  "relative isolate rounded-2xl ring-1 ring-foreground/8 shadow-[0_10px_30px_rgb(20_20_30/0.06)] dark:ring-foreground/15 dark:shadow-[0_10px_30px_rgb(0_0_0/0.6)] before:pointer-events-none before:absolute before:inset-0 before:-z-10 before:rounded-[inherit] before:bg-background/75 before:shadow-[inset_0_1px_0_rgb(255_255_255/0.65)] before:backdrop-blur-xl before:backdrop-saturate-150 dark:before:bg-accent/70 dark:before:shadow-[inset_0_1px_0_rgb(255_255_255/0.12)]";
