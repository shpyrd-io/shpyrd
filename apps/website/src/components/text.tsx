import React from "react";
import Markdoc from "@markdoc/markdoc";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { PageLayoutContent, PageLayoutPane } from "@shpyrd/ui/components/page-layout";
import { Prose } from "@shpyrd/ui/components/prose";
import type { Text as TextOf } from "@/lib/content";
import { components } from "./markdoc";
import { OnThisPage } from "./on-this-page";

// A text of the site as a page: its heading, the text itself, and beside
// it the list of its parts. Rendered when the application is built.
export function Text({ text }: { text: TextOf }) {
  // The parts, and the sections of a part under it, a step in. Deeper headings
  // are too small a piece to go to from the side.
  const parts = text.headings.filter((h) => h.level === 2 || h.level === 3);
  return (
    <>
      <PageLayoutContent width="xlarge" padding="normal" className="grid grid-cols-1 content-start gap-8">
        <PageHeading title={text.title} description={text.description} variant="large" border />
        <Prose>{Markdoc.renderers.react(text.content, React, { components })}</Prose>
      </PageLayoutContent>
      {parts.filter((h) => h.level === 2).length > 1 && (
        <PageLayoutPane
          aria-label="On this page"
          position="end"
          width="small"
          sticky
          // The site header stays at the top too: its 3.5rem, the room held
          // above it and its line, 4.75rem in all.
          offsetHeader="4.75rem"
          hidden={{ narrow: true }}
          className="py-6 pr-4"
        >
          <OnThisPage parts={parts.map(({ id, title, level }) => ({ id, title, level }))} />
        </PageLayoutPane>
      )}
    </>
  );
}
