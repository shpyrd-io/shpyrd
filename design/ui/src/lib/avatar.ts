// What an avatar is made of, without the component: the sizes, the
// initials, the radius and the size of the letters. Here so that what is
// rendered on the server may use them too.

// From 16 to 32 by four, then by eight; 64 is the largest.
export const sizes = [16, 20, 24, 28, 32, 40, 48, 64] as const;
export type Size = (typeof sizes)[number];

// The first letter of the first and of the last word: "Ana Souza" is "AS".
export function initials(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const first = words[0]?.[0] ?? "";
  const last = words.length > 1 ? (words[words.length - 1]?.[0] ?? "") : "";
  return (first + last).toUpperCase();
}

// The radius: a circle, or a square with a corner that follows its size.
export function shape(size: number, square: boolean) {
  return square ? (size < 24 ? "rounded-sm" : "rounded-md") : "rounded-full";
}

// The size of the letters, for the initials and for a count.
export function letters(size: number) {
  return Math.max(8, Math.round(size * 0.4));
}
