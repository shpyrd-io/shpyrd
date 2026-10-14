import Link from "next/link";
import { notFound } from "next/navigation";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Hero } from "@shpyrd/ui/components/hero";
import { Prose } from "@shpyrd/ui/components/prose";
import { changelogPosts, changelogCopy, changelogDate } from "@/lib/mdx/changelog";
import { components } from "@/components/mdx";

export const dynamicParams = false;
export async function generateStaticParams() {
  return (await changelogPosts()).map(({ slug }) => ({ slug }));
}
async function getPost(slug: string) {
  const post = (await changelogPosts()).find((post) => post.slug === slug);
  if (!post) notFound();
  return post;
}
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const post = await getPost((await params).slug);
  return { title: post.title, description: post.description, alternates: { canonical: `/changelog/${post.slug}` } };
}
export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const post = await getPost((await params).slug);
  return <PageLayoutContent width="large" padding="normal" className="grid gap-10 py-10 sm:py-16">
    <Link href="/changelog" className="w-fit text-sm text-muted-foreground hover:text-foreground">← {changelogCopy.back}</Link>
    <Hero variant="page" heading={post.title} description={<time dateTime={post.date}>{changelogDate(post.date)}</time>} align="center" />
    <Prose className="mx-auto w-full max-w-3xl"><post.Content components={components} /></Prose>
  </PageLayoutContent>;
}
