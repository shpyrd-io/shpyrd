import * as React from "react";

// A text that was written, not built: the headings, paragraphs, lists
// and tables of a document take the look of the library, whatever made
// them. What it looks like is in `styles/prose.css`.
//
// A component of the library put inside keeps its own look. `html` is
// shown as it comes: what is not trusted has to be cleaned before.
function Prose({
  html,
  fullWidth = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  // The text as HTML, in place of children.
  html?: string;
  // Without it a line has at most the length that is good to read.
  fullWidth?: boolean;
}) {
  if (html !== undefined) {
    return (
      <div
        data-slot="prose"
        data-full-width={fullWidth}
        dangerouslySetInnerHTML={{ __html: html }}
        {...props}
      />
    );
  }
  return (
    <div data-slot="prose" data-full-width={fullWidth} {...props}>
      {children}
    </div>
  );
}

export { Prose };
