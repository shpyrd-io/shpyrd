# The emails the platform sends

Every email shpyrd sends, drawn here: invitations, a password reset, a
changed password, the test message, and shpyrd cloud's sign-up code and
notices to a workspace's owners. They are listed once, in
`src/emails.tsx`, each with its subject, its words and sample words for
them, and made of the parts in `src/parts.tsx`.

An email is not a page: a mail client reads no stylesheet it can rely on,
no custom property and no flexbox. So nothing of `design/ui` is drawn in
an email; every style is written on its element, what is laid out is a
table, and the colours are the library's tokens turned into hex.

`npm run dev` (port 4328, `design-emails` in `.claude/launch.json`) opens
a gallery made like `design/ui`'s: each email as the inbox shows it, in a
frame as tall as the email, wide and on a phone, and the words the server
fills in. Each email is drawn at two addresses:

- `/template/<name>`: the words are a Go template's marks (`{{.Door}}`).
  This is what is packed.
- `/preview/<name>`: the words are samples.

`npm run build` exports them and `scripts/pack.mjs` takes each email out
of its page into a document of its own, in `pkg/emails/html/`, with the
subjects in `subjects.json` beside them. `pkg/emails` embeds the files;
a sender calls `emails.Render` with the words, and keeps the plain-text
part of the message its own. The files are committed; run `make emails`
after changing an email.

The mark is an image, `{{.Logo}}`: `logo.png` on the door the email is
sent from, which the applications serve (`apps/*/public/logo.png`, the
PNG of `design/brand/logo`). SVG is not drawn by most mail clients.
