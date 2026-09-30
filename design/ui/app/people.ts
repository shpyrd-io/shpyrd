// Who the gallery shows. Made up, and drawn the same way every time: the
// pictures are small SVGs, so the gallery asks nothing of the network.

export type Person = { alt: string; src?: string; square?: boolean };

function hue(seed: number) {
  return Math.round((seed * 137.508) % 360);
}

// A person: a head and shoulders over a colour.
function portrait(seed: number) {
  const h = hue(seed);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="hsl(${h} 55% 52%)"/><circle cx="32" cy="25" r="12" fill="hsl(${h} 60% 88%)"/><path d="M8 66a24 24 0 0 1 48 0z" fill="hsl(${h} 60% 88%)"/></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

// What is not a person: a mark over a colour.
function emblem(seed: number) {
  const h = hue(seed);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="hsl(${h} 30% 30%)"/><path d="M32 14l16 9v18l-16 9-16-9V23z" fill="none" stroke="hsl(${h} 60% 85%)" stroke-width="5"/></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

export const people: Person[] = [
  { alt: "Ana Souza", src: portrait(1) },
  { alt: "Marcelo Lima", src: portrait(2) },
  { alt: "Beatriz Costa", src: portrait(3) },
  { alt: "Rafael Alves", src: portrait(4) },
  { alt: "Carla Mendes", src: portrait(5) },
  { alt: "Diego Ramos", src: portrait(6) },
  { alt: "Helena Rocha", src: portrait(7) },
];

export const teams: Person[] = [
  { alt: "Platform", src: emblem(11), square: true },
  { alt: "Billing", src: emblem(12), square: true },
  { alt: "shpyrd bot", src: emblem(13), square: true },
];

// One without a picture: the initials are drawn.
export const anonymous: Person = { alt: "Otávio Pires" };
