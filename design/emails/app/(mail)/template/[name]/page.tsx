import { notFound } from "next/navigation";
import { mails, marks } from "@/src/emails";

// An email with a Go template's marks for its words: what
// scripts/pack.mjs packs for pkg/emails.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(mails).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const mail = mails[(await params).name];
  if (!mail) notFound();
  return mail.render(marks(mail));
}
