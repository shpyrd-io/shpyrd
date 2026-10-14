import { ChangelogPage } from "@/components/changelog";
import { changelogCopy, changelogPosts } from "@/lib/mdx/changelog";
export const metadata = { title: changelogCopy.title, description: changelogCopy.description, alternates: { canonical: "/changelog" } };
export default async function Page() { return <ChangelogPage posts={await changelogPosts()} />; }
