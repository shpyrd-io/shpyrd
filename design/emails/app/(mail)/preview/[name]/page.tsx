import { notFound } from "next/navigation";
import { mails, sample } from "@/src/emails";

// An email with sample words, as a person would get it.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(mails).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const mail = mails[(await params).name];
  if (!mail) notFound();
  return mail.render(sample(mail));
}
