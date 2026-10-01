import React from "react";
import Markdoc from "@markdoc/markdoc";
import { NavList, NavListItem } from "@shpyrd/ui/components/nav-list";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { PageLayoutContent, PageLayoutPane } from "@shpyrd/ui/components/page-layout";
import { Prose } from "@shpyrd/ui/components/prose";
import type { Text as TextOf } from "@/lib/content";
import { components } from "./markdoc";

// A text of the site as a page: its heading, the text itself, and beside
// it the list of its parts. Rendered when the application is built.
export function Text({ text }: { text: TextOf }) {
  // The parts, and the sections of a part under it, a step in. Deeper headings
  // are too small a piece to go to from the side.
  const parts = text.headings.filter((h) => h.level === 2 || h.level === 3);
  return (
    <>
      <PageLayoutContent width="large" padding="normal" className="grid content-start gap-8">
        <PageHeading title={text.title} description={text.description} variant="large" border />
        <Prose>{Markdoc.renderers.react(text.content, React, { components })}</Prose>
      </PageLayoutContent>
      {parts.filter((h) => h.level === 2).length > 1 && (
        <PageLayoutPane
          aria-label="On this page"
          position="end"
          width="small"
          sticky
          // The site header stays at the top too: 3.5rem and its line.
          offsetHeader="calc(3.5rem + 1px)"
          hidden={{ narrow: true }}
          className="py-6 pr-4"
        >
          <NavList aria-label="On this page" heading="On this page" headingLevel="h2">
            {parts.map((h) => (
              <NavListItem key={h.id} asChild className={h.level === 3 ? "pl-5" : undefined}>
                <a href={`#${h.id}`}>{h.title}</a>
              </NavListItem>
            ))}
          </NavList>
        </PageLayoutPane>
      )}
    </>
  );
}
