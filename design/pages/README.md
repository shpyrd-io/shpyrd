# The server's own pages

The pages the Go server serves by itself, where no application answers:
nothing here, no access, waking up, service unavailable, and the mark
alone (the root of the sign-in host). They are the design guide's state
pages (`design/ui`, Layouts & Pages, State pages), drawn from the same
`StatePage` component, so a change to the component reaches them.

`npm run dev` (port 4327, or `PORT`; `design-pages` in
`.claude/launch.json` uses 4329) opens a gallery made like `design/ui`'s:
each page in a frame, light and dark, wide and on a phone, with sample
words. The pages are listed once, in `src/pages.tsx`, and drawn at two
addresses:

- `/template/<kind>`: the words are a Go template's marks (`{{.Title}}`,
  `{{.Text}}`, the links). This is what is packed.
- `/preview/<kind>`: the words are samples, as a person would see them.

`npm run build` exports them and `scripts/pack.mjs` packs each template
into one file in `pkg/pages/html/`: the stylesheet inline, no font to
fetch (the system's sans-serif), the dark theme the system's (the library
draws it under a `.dark` class; pack turns it into a media query).
`pkg/pages` embeds the files and fills the words. The files are
committed; run `make pages` after changing the component or these pages.

A page has no script, but one that moves: the service-unavailable page
draws the shipyard of `design/ui` with `scripts/shipyard.ts`, the
component's world and scene without React, bundled with the files of its
pieces into `pkg/pages/html/service-unavailable.js`. The server writes it
at the end of the page and names its hash in the page's
Content-Security-Policy, so the page needs nothing from a host that is not
answering. A change to how `shipyard.tsx` draws is made in the script too.
