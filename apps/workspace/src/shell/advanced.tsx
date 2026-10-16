"use client";

import { Link } from "react-router-dom";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { PageHeading } from "@shpyrd/ui/components/page-heading";

export type AdvancedPage = { group: string; slug: string; title: string; icon: React.ReactElement };

// Advanced, of a project or of the workspace: every page, by what it is
// for, each with a line that says it.
export function AdvancedIndex({ base, description, pages, lines }: { base: string; description: string; pages: readonly AdvancedPage[]; lines: Record<string, string> }) {
  const groups = [...new Set(pages.map((p) => p.group))];
  return (
    <div className="grid gap-8">
      <PageHeading as="h2" variant="subtitle" title="Advanced" description={description} />
      {groups.map((group) => (
        <section key={group} className="grid gap-3">
          <h3 className="text-xs font-medium text-muted-foreground uppercase">{group}</h3>
          <div className="grid gap-3 @xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-3">
            {pages
              .filter((p) => p.group === group)
              .map((p) => (
                <Card key={p.slug} size="sm" asChild>
                  <Link to={`${base}/${p.slug}`}>
                    <CardContent className="flex gap-3 [&>svg]:mt-0.5 [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-muted-foreground">
                      {p.icon}
                      <div className="min-w-0">
                        <div className="font-medium">{p.title}</div>
                        <div className="text-muted-foreground">{lines[p.slug]}</div>
                      </div>
                    </CardContent>
                  </Link>
                </Card>
              ))}
          </div>
        </section>
      ))}
    </div>
  );
}
