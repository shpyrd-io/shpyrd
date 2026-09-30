import { ClientOnly } from "./client";

// One document: the server answers every address with it, and the
// application reads the address in the browser.
export function generateStaticParams() {
  return [{ slug: [""] }];
}

export default function Page() {
  return <ClientOnly />;
}
