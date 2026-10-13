import Link from "next/link";
import { Changelog, ChangelogEntry, ChangelogSummary } from "@shpyrd/ui/components/changelog";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { changelogCopy as copy, changelogDate, type ChangelogPost } from "@/lib/mdx/changelog";
import { components } from "./mdx";
import { ChangelogArchive } from "@shpyrd/ui/components/changelog-archive";

export function ChangelogPage({ posts }: { posts: ChangelogPost[] }) {
  const entry = (post: ChangelogPost) => (
    <ChangelogEntry key={post.slug} id={post.slug} className="scroll-mt-24" title={<Link href={`/changelog/${post.slug}`} className="hover:underline underline-offset-4">{post.title}</Link>}
      date={<time dateTime={post.date}>{changelogDate(post.date)}</time>} tag={post.type}>
      <post.Content components={components} />
    </ChangelogEntry>
  );
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-12 py-10 sm:py-16">
      <Hero variant="page" heading={copy.title} description={copy.description} align="center" />
      {posts.length > 0 ? <Changelog>{posts.slice(0, 3).map((post) => entry(post))}</Changelog>
        : <p className="text-muted-foreground">{copy.empty}</p>}
      <ChangelogArchive title={copy.archive} loadMore={copy.loadMore}
        entries={posts.slice(3).map((post) => ({ slug: post.slug, year: post.date.slice(0, 4), content: <ChangelogSummary key={post.slug} title={<Link href={`/changelog/${post.slug}`}>{post.title}</Link>}
          date={<time dateTime={post.date}>{changelogDate(post.date, false)}</time>} tag={post.type}>{post.description}</ChangelogSummary> }))} />
    </PageLayoutContent>
  );
}
