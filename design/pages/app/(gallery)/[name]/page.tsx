import { notFound } from "next/navigation";
import { pages } from "@/src/pages";
import { Frame } from "../frame";
import { Section } from "../section";

// One page: in a window, light and dark, and on a phone.
export const dynamicParams = false;

export function generateStaticParams() {
  return Object.keys(pages).map((name) => ({ name }));
}

export default async function Page({ params }: { params: Promise<{ name: string }> }) {
  const { name } = await params;
  const page = pages[name];
  if (!page) notFound();
  const src = `/preview/${name}`;
  return (
    <>
      <Section title="Light">
        <Frame src={src} width="100%" height={620} title={`${page.title}, light`} />
      </Section>
      <Section title="Dark">
        <Frame src={src} width="100%" height={620} dark title={`${page.title}, dark`} />
      </Section>
      <Section title="On a phone">
        <div className="flex flex-wrap gap-6">
          <Frame src={src} width={375} height={700} title={`${page.title}, on a phone, light`} />
          <Frame src={src} width={375} height={700} dark title={`${page.title}, on a phone, dark`} />
        </div>
      </Section>
      <p className="text-sm text-muted-foreground">
        The template:{" "}
        <a href={`/template/${name}`} className="text-primary underline-offset-4 hover:underline">
          /template/{name}
        </a>
      </p>
    </>
  );
}
