# Contact pages: sales and enterprise

Date: 2026-10-09
Status: draft, awaiting review
Scope: `apps/website`, `content/site`, one component in `design/ui`

## Intent

Every "contact" on the site leads to the project's Discord today
(`content/site/offer.ts`, `contact.href`): the pricing table's "Talk to us",
the Enterprise plan's button, and four links on the Solutions pages. A
company that wants to buy, or to run shpyrd in its own cloud, has no way to
say so but to join a chat.

When this is done the site has two contact pages, like Supabase's
[sales page](https://supabase.com/contact/sales) and its
[enterprise form](https://forms.supabase.com/enterprise), inside the site
rather than on a domain of their own:

- `/contact/sales`, for anyone who wants to talk about using shpyrd;
- `/contact/enterprise`, a fuller form for shpyrd in a company's own cloud,
  single sign-on, reviews and contracts.

A form sent reaches the team as an email, through Mailgun.

## Decisions

| Subject | Decision |
|---|---|
| Where the answers go | an email to an inbox, sent through Mailgun's SMTP |
| Where the mailer runs | the website itself: a route handler on Vercel; the site stops being a static export |
| Beside the forms | what happens next, in words; no customer quotes or logos |
| The links' wording | "Contact us" in place of "Talk to us", everywhere on the site |
| New components | in `design/ui`, by its rules; the website uses the library through the workspace |

The website was a static export: "Next, for everything, compiled to static
files; no Node at run time" (the
[design, content and apps plan](../plans/2026-09-29-design-content-apps.md)).
The site is now the one exception, with one route on request; every page is
still rendered when the site is built.

## The pages

Both are pages of the site, with its header and footer, drawn with
`design/ui`: the form on the left; on the right, what happens next. Their
text lives in `content/site/contact.ts`, like the rest of the site's.

### `/contact/sales`

- Heading: **Contact sales**. Under it: "Tell us about the apps your team
  would bring, and we'll set up a call."
- Fields:

  | Field | Kind | Required |
  |---|---|---|
  | First name | text | yes |
  | Last name | text | yes |
  | Company email | email | yes |
  | How can we help you? | text, several lines | yes |

- Button: **Request a call**.
- Beside it, with `ChecklistItems`: what the call covers (the apps you
  would bring, who should reach them, where they would run), and what
  follows (a reply within one business day; then a trial workspace on
  shpyrd cloud, or a plan for your own cloud). Under it: "Technical
  question? Ask in Discord", to `/discord`.

### `/contact/enterprise`

- Heading: **Contact enterprise sales**. Under it, a sentence on shpyrd in
  a company's own cloud, on its terms.
- Fields:

  | Field | Kind | Required |
  |---|---|---|
  | First name | text | yes |
  | Last name | text | yes |
  | Company email | email | yes |
  | Company | text | yes |
  | Job title | text | yes |
  | Company size | select: 1–49 · 50–249 · 250–999 · 1000+ | yes |
  | Where would shpyrd run? | select: shpyrd cloud · Our own cloud (AWS, Oracle Cloud…) · On-premises · Not sure yet | yes |
  | Timeline | select: Exploring · This quarter · Next quarter or later | yes |
  | What do you need? | checkboxes: Single sign-on · Security or compliance review · Support with an SLA · Invoicing and procurement · Managed for our clients | no |
  | Tell us about your setup | text, several lines | no |

- Button: **Contact enterprise sales**.
- Beside it: the Enterprise plan's points, from
  `content/site/pricing.ts` (sign-in through your own provider, apps and
  databases that sleep, costs per project, the MCP server for AI
  assistants), and what follows (a reply within one business day, a call,
  a license for a trial).

### Both

- Under the button: "By submitting, you agree to our Privacy Policy",
  linked to `https://legal.shpyrd.io/global/privacy-policy`, the address
  the footer already uses.
- The fields are checked as they are filled and before sending, with the
  same rules the server applies; an error shows under its field.
- Sent: the form gives way to "Thanks, we'll reply to <email> within one
  business day."
- Not sent: a message over the form, which keeps what was typed, and the
  way to Discord.
- On success the page pushes `{ event: "contact_submitted", form: "sales" | "enterprise" }`
  to the `dataLayer` the site already gives Google Tag Manager.

### The links

| Where | Today | After |
|---|---|---|
| The Enterprise plan's button (`content/site/pricing.ts`) | "Talk to us", to Discord | "Contact us", to `/contact/enterprise` |
| Every other `contact` link (`offer.ts`'s `contact`, the pricing table's line, the Solutions pages' four `href: 'contact'`) | to Discord | to `/contact/sales` |
| Any other "Talk to us" in `content/site` and `apps/website` | "Talk to us" | "Contact us" |

Discord stays where it is in the footer, and in the pages' "Technical
question?" line.

## Sending

### The site with one route

- `apps/website/next.config.ts` no longer sets `output: "export"`. Next
  renders every page when the site is built, as it does by default; only
  the route handler runs on request, as a Vercel function.
- The redirects stay in `vercel.json`, which Vercel applies before anything
  else.
- The route: `app/api/contact/route.ts`, `POST`, on Node.js.

### One set of rules

`src/lib/contact.ts` defines both forms: their fields, which are required,
the values each select and each checkbox allows, the email's shape, and the
lengths (100 characters a field, 500 for the texts of several lines). The
page uses it to show errors before sending; the route uses it to refuse
anything else with a `400` and the errors field by field.

### The mail

`nodemailer`, through Mailgun's SMTP (`smtp.mailgun.org`, port 587,
STARTTLS), configured by environment variables in Vercel:

| Variable | Example |
|---|---|
| `SMTP_HOST` | `smtp.mailgun.org` |
| `SMTP_PORT` | `587` |
| `SMTP_USER` | `postmaster@mg.shpyrd.io` |
| `SMTP_PASS` | Mailgun's SMTP password |
| `CONTACT_FROM` | `shpyrd website <website@mg.shpyrd.io>` |
| `CONTACT_TO` | `sales@shpyrd.io` |

- Subject: `[Sales] Ana Souza, acme.com` (the sales form asks no company: the email's domain says which), or
  `[Enterprise] Ana Souza, Acme (250–999): our own cloud`.
- Body, as text and as a simple table: every field, then the page it came
  from, the time, and the country Vercel reports (`x-vercel-ip-country`).
- `Reply-To` is the address the person gave, so a reply from the inbox
  reaches them.
- Nothing is ever sent to the address someone typed: the form cannot be
  used to send mail to others.

### Against spam

- A field people do not see and bots fill in.
- At least three seconds between the page opening and the form being sent.
- The server's own check of every field.

A submission refused for the first two is answered as if it were sent, so a
bot learns nothing. If spam gets through, Cloudflare Turnstile can be added.

### When it fails

| Case | Answer | The page |
|---|---|---|
| A field wrong | `400` with the errors | the errors under their fields |
| Variables not set (a Vercel preview, say) | `503` | "Could not send", and Discord |
| Mailgun refuses or does not answer | `502`, logged with the reason, never the content | "Could not send", what was typed kept, and Discord |
| Sent | `200` | the thanks |

Previews have no variables, so they never send real mail.

## A component for the library

The enterprise form's "What do you need?" needs checkboxes, and
`design/ui` has none (`checklist.tsx` is a list of a feature's points, not
an input). `Checkbox` goes into `design/ui/src/components`, by the library's
rules (`design/DESIGN_FOR_AGENT.md`): a gallery page and its line in
`app/catalog.ts`, tests, `"use client"`, colours by name, and a line under
`## Unreleased` in `design/ui/CHANGELOG.md`. The website uses it through the
workspace, without waiting for a release.

Anything else generic that building the pages calls for (the layout of a
form beside what happens next, if it is) goes into `design/ui` the same way;
what is only these pages' stays in the site.

## Checks

- Tests, beside the code:
  - `src/lib/contact.ts`: a required field missing, a bad email, a value no
    select or checkbox offers, a text too long, the hidden field filled,
    sent too fast;
  - the route, with a stand-in mailer: the subject, `Reply-To` and body it
    builds; that it never mails the typed address; its `400`, `503`, `502`
    and `200`;
  - the pages: their fields, the errors, the thanks;
  - `Checkbox`: checked and unchecked, by click and by keyboard, its label,
    disabled.
- The site's lint, type-check, tests and build; the build lists every page
  as prerendered and only `/api/contact` as a function.
- By hand: both pages light and dark and at a phone's width; one real send
  through Mailgun to a test inbox once the credentials exist; every
  "Contact us" leading to its page.

## Documents that change

| Document | Change |
|---|---|
| `apps/website/README.md` | the route, its variables, and that the site is no longer a static export |
| `docs/superpowers/plans/2026-09-29-design-content-apps.md` | the "Framework" row: the website's one route on Vercel |
| `design/ui/CHANGELOG.md` | `Checkbox` under `## Unreleased` |

## The owner's steps

- In Vercel's project settings, the output directory must not be set to
  `out`, the static export's folder; if it is, clear it before merging, or
  the deploy fails.
- A Mailgun sending domain (for example `mg.shpyrd.io`) and its SMTP
  credentials.
- The six variables in Vercel, for Production only.

## Risks

- Spam past the light protection: Turnstile.
- The privacy policy should say what the contact forms collect and why;
  that is for `legal`.

## Not in this work

- A confirmation email to the person who wrote.
- Storing the submissions anywhere but the inbox, or sending them to a CRM.
- Customer quotes or logos.
