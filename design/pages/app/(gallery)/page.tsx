import Link from "next/link";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { pages } from "@/src/pages";
import { Frame } from "./frame";

// The way in: every page, small.
export default function Overview() {
  return (
    <>
      <PageHeading title="Overview" description="The pages the server serves by itself, where no application answers, with sample words." />
      <div className="grid gap-6 @3xl/page-layout:grid-cols-2 @7xl/page-layout:grid-cols-3">
        {Object.entries(pages).map(([name, page]) => (
          <Link key={name} href={`/${name}`} className="group grid content-start gap-2">
            <div className="pointer-events-none h-60 overflow-hidden rounded-lg border transition-colors group-hover:border-primary">
              <div className="w-[200%] origin-top-left scale-50">
                <Frame src={`/preview/${name}`} width="100%" height={480} title={page.title} />
              </div>
            </div>
            <div className="text-sm font-medium">{page.title}</div>
            <div className="text-xs text-muted-foreground">{page.about}</div>
          </Link>
        ))}
      </div>
    </>
  );
}
