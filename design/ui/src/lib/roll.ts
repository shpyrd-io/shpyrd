import { cn } from "cn";

// A roll: words (or a mark and its words) that take turns in one place,
// the one leaving going up and out while the next comes in from under it.
// Every item sits in one grid cell that clips (`overflow-hidden`), each with
// `data-place`: "here" (showing), "leaving", or "waiting" (under the cell,
// unseen, and back there without a transition). In the library's names for
// motion: the roll is a move (`duration-slow`, `ease-move`).
export const roll = cn(
  "transition-[translate,opacity,filter] duration-slow ease-move",
  "data-[place=here]:translate-y-0 data-[place=here]:opacity-100 data-[place=here]:blur-none",
  "data-[place=leaving]:-translate-y-[110%] data-[place=leaving]:opacity-0 data-[place=leaving]:blur-[3px]",
  "data-[place=waiting]:translate-y-[110%] data-[place=waiting]:opacity-0 data-[place=waiting]:blur-[3px] data-[place=waiting]:transition-none",
);

// Where an item is in the roll, by its index and the clock's.
export const placeIn = (i: number, index: number, leaving: number) =>
  i === index ? "here" : i === leaving ? "leaving" : "waiting";
