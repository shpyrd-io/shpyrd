// The site's informative table, on the library's Table inside a glass Card:
// the look of the comparison table (src/components/compare-table.tsx) without
// its lit column. Rows parted by the footer's hairline, no hover; the cards'
// padding (24px across, 20px down), its cells on one baseline; the head in
// the description's grey;
// each row's name, the first cell, in the cards' title type; the rest in their
// body type, wrapping as it needs.
export const infoTable = [
  "[&_[data-slot=table-row]]:border-foreground/8 dark:[&_[data-slot=table-row]]:border-foreground/15 [&_[data-slot=table-row]]:hover:bg-transparent",
  "[&_[data-slot=table-head]]:h-auto [&_[data-slot=table-head]]:px-6 [&_[data-slot=table-head]]:py-5 [&_[data-slot=table-head]]:text-sm [&_[data-slot=table-head]]:font-medium [&_[data-slot=table-head]]:text-muted-foreground",
  "[&_[data-slot=table-cell]]:px-6 [&_[data-slot=table-cell]]:py-5 [&_[data-slot=table-cell]]:align-baseline [&_[data-slot=table-cell]]:text-sm [&_[data-slot=table-cell]]:whitespace-normal",
  "[&_[data-slot=table-cell]:first-child]:font-heading [&_[data-slot=table-cell]:first-child]:text-base [&_[data-slot=table-cell]:first-child]:font-semibold [&_[data-slot=table-cell]:first-child]:text-foreground",
].join(" ");
