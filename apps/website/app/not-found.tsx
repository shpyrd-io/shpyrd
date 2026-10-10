import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Shipyard } from "@shpyrd/ui/components/shipyard";
import { StatePage } from "@shpyrd/ui/components/state-page";
import { notFound } from "@shpyrd/content/site/not-found";

// Any address the site does not have: the yard at work, and no container
// for it, in the server's own state page (design/pages), between the site's
// header and footer. The way home first, then the docs and Discord.
export const metadata = { title: "Page not found" };

export default function NotFound() {
  const [home, ...others] = notFound.links;
  return (
    <PageLayoutContent width="xlarge" padding="normal">
      <StatePage
        // Its own wordmark in the corner is for the server's pages, which have no
        // footer; the site's footer carries one already.
        className="min-h-[calc(100svh-14rem)] bg-transparent [&>.absolute.bottom-5]:hidden"
        picture={<Shipyard className="w-72 max-w-[70vw]" />}
        title={notFound.title}
        description={notFound.description}
        action={
          <>
            <Button asChild size="lg" iconEnd={<ArrowRight />}>
              <Link href={home.href}>{home.label}</Link>
            </Button>
            {others.map((l) => (
              <Button key={l.href} asChild size="lg" variant="outline">
                <Link href={l.href}>{l.label}</Link>
              </Button>
            ))}
          </>
        }
      />
    </PageLayoutContent>
  );
}
