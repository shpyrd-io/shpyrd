# The server's own pages

The pages the Go server serves where no application answers: nothing
here, no access, waking up, and the mark alone (the root of the sign-in
host). They are the design guide's state pages (`design/ui`, Layouts &
Pages, State pages), drawn from the same `StatePage` component, so a
change to the component reaches them.

Each page is a Next route here (`app/<kind>/page.tsx`) whose words are a
Go template's marks (`{{.Title}}`, `{{.Text}}`, the links). `npm run
build` exports them and `scripts/pack.mjs` packs each into one file in
`pkg/pages/html/`: the stylesheet inline, no script, no font to fetch
(the system's sans-serif), the dark theme the system's. `pkg/pages`
embeds the files and fills the words. The files are committed; run `make
pages` after changing the component or these routes.

`npm run dev` shows the routes with the template's marks as they are.
