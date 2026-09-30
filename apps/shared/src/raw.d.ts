// A file imported as text, the way Vite (so vitest) reads `?raw`.
declare module "*?raw" {
  const text: string;
  export default text;
}
