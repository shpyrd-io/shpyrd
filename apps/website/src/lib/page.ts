// The rhythm of a page of the site, from the home page: 168px between its
// sections (the grid's 64px and 104px more), 64px from the hero to the first
// of them, and 168px from the last to the footer's line. The CTA block keeps
// its own, closer room (src/components/shipyard-cta.tsx), and so does the
// ending of a subpage, which sits on the footer (subpage-step.tsx); an
// overview's next-page button stands right under its closing block. In a
// section, 56px from its title to what follows it (the section's 24px gap and
// 32px more).
export const pageSections =
  "grid grid-cols-1 content-start gap-16 py-8 [&>*+*]:mt-26 [&>[data-slot=hero]+*]:mt-0 [&>*:last-child]:mb-36 [&>[data-slot=cta]]:mt-0 [&>[data-slot=cta]]:mb-0 [&>[data-slot=next-step]]:mb-0 [&>[data-slot=cta]+[data-slot=next-step]]:mt-0 [&>[data-slot=next-step]+[data-slot=next-step]]:mt-0 [&>*>[data-slot=section-intro]+*]:mt-8";
