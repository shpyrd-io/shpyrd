# Contact pages: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two contact pages on the website, `/contact/sales` and `/contact/enterprise`, whose forms reach the team as email through Mailgun; every "contact" on the site leads to them, as "Contact us".

**Architecture:** The forms are defined once, as content (`content/site/contact.ts`: fields, labels, choices, copy); `apps/website/src/lib/contact.ts` derives the rules both the page and the server apply. A client form posts to one route handler, `app/api/contact/route.ts`, which builds the email (`src/lib/contact-mail.ts`) and sends it through `nodemailer` and Mailgun's SMTP (`src/lib/mailer.ts`). The website stops being a static export; every page is still rendered at build time.

**Tech Stack:** Next 16 (App Router) on Vercel, React 19, `design/ui` (`@shpyrd/ui`), `@shpyrd/content`, nodemailer, vitest, Testing Library, jsdom.

**Spec:** `docs/superpowers/specs/2026-10-09-contact-pages-design.md`

## Global Constraints

- Brand name in text is always lowercase: shpyrd.
- Links read "Contact us"; no "Talk to us" remains in `content/site` or `apps/website`.
- Lengths: 100 characters a field, 500 for a text of several lines.
- Nothing is ever mailed to the address a person typed: `to` is always `CONTACT_TO`; the typed address is `Reply-To` only.
- Environment variables: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `CONTACT_FROM`, `CONTACT_TO`. Missing any but `SMTP_PORT` (default 587): the route answers `503`.
- Spam: the hidden field is named `website`; the least time between opening and sending is 3,000 ms; a submission caught by either is answered `200` as if sent, and nothing is mailed.
- Privacy link: `https://legal.shpyrd.io/global/privacy-policy`.
- New components go into `design/ui` by its rules (`design/DESIGN_FOR_AGENT.md`, "Rules of the library"), with a line under `## Unreleased` in `design/ui/CHANGELOG.md`.
- Copy lives in `content/site`, not in components.

## Review Focus

1. **A line break typed into a name or company.** Expected: the email's subject stays one line; nothing typed can add a mail header. Pinned in Task 4 (`contactMail` strips CR/LF from the subject).
2. **An answer with a value no select offers, or a field the form does not have.** Expected: the server refuses with `400` for the first; ignores the second (never mails it). Pinned in Task 3 (`check`) and Task 4 (`contactMail` only reads the form's fields).
3. **HTML typed into a field.** Expected: the email's HTML shows it as text, not as markup. Pinned in Task 4 (escaped table cells).
4. **A body that is not JSON, or no `kind`.** Expected: `400`, not a crash. Pinned in Task 5.
5. **Mailgun failing.** Expected: `502`, the reason logged, the content never logged; the page keeps what was typed. Pinned in Task 5 (route) and Task 7 (form).

---

Work on the branch `feat/contact-pages` (it holds the spec). Run commands from the repository root unless a step says otherwise.

### Task 1: Checkbox in design/ui

**Files:**
- Create: `design/ui/src/components/checkbox.tsx`
- Create: `design/ui/src/components/checkbox.test.tsx`
- Create: `design/ui/app/structures/checkbox/page.tsx`
- Modify: `design/ui/app/catalog.ts` (the `structures` pages, after `switch`)
- Modify: `design/ui/CHANGELOG.md` (under `## Unreleased`)

**Interfaces:**
- Produces: `Checkbox` from `@shpyrd/ui/components/checkbox`, the props of Radix's `Checkbox.Root` (`checked`, `defaultChecked`, `onCheckedChange(checked: boolean | "indeterminate")`, `disabled`, `id`, `name`, `value`, `aria-invalid`). Task 7 uses it.

- [ ] **Step 1: Write the failing tests**

`design/ui/src/components/checkbox.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Checkbox } from "./checkbox";
import { Label } from "./label";

describe("Checkbox", () => {
  it("is not checked until it is clicked, and says so when it is", () => {
    const onCheckedChange = vi.fn();
    render(<Checkbox aria-label="Single sign-on" onCheckedChange={onCheckedChange} />);
    const box = screen.getByRole("checkbox", { name: "Single sign-on" });
    expect(box.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("is named by the label that points at it", () => {
    render(
      <>
        <Checkbox id="sla" />
        <Label htmlFor="sla">Support with an SLA</Label>
      </>,
    );
    expect(screen.getByRole("checkbox", { name: "Support with an SLA" })).toBeTruthy();
  });

  it("cannot be changed while disabled", () => {
    render(<Checkbox aria-label="Managed" disabled />);
    const box = screen.getByRole("checkbox", { name: "Managed" });
    fireEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("false");
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd design/ui && npx vitest run src/components/checkbox; cd ../..`
Expected: FAIL, `Failed to resolve import "./checkbox"`.

- [ ] **Step 3: Write the component**

`design/ui/src/components/checkbox.tsx`:

```tsx
"use client";

import * as React from "react";
import { Check } from "lucide-react";
import { Checkbox as CheckboxPrimitive } from "radix-ui";
import { cn } from "cn";

// One of several choices in a form that is sent later. A setting that
// takes effect at once is a Switch. Its words are a Label that points at
// its id, beside it.
function Checkbox({ className, ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer size-4 shrink-0 rounded-[4px] border border-input bg-transparent text-primary-foreground transition-colors outline-none",
        "focus-visible:border-foreground focus-visible:ring-3 focus-visible:ring-foreground/10",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20",
        "data-[state=checked]:border-primary data-[state=checked]:bg-primary",
        "dark:bg-input/30",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator" className="grid place-content-center">
        <Check aria-hidden className="size-3.5" strokeWidth={3} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd design/ui && npx vitest run src/components/checkbox; cd ../..`
Expected: PASS, 3 tests.

- [ ] **Step 5: Its page in the gallery, its line, its CHANGELOG line**

`design/ui/app/structures/checkbox/page.tsx`:

```tsx
import { Checkbox } from "@shpyrd/ui/components/checkbox";
import { Label } from "@shpyrd/ui/components/label";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";

const needs = ["Single sign-on", "Security or compliance review", "Support with an SLA", "Invoicing and procurement"];

export default function Page() {
  return (
    <>
      <Section title="With a label">
        <Stack direction="horizontal" align="center" gap="tight">
          <Checkbox id="terms" />
          <Label htmlFor="terms">Send me the product updates</Label>
        </Stack>
      </Section>
      <Section title="Several choices">
        <fieldset className="grid gap-3">
          <legend className="mb-1 text-sm font-medium">What do you need?</legend>
          {needs.map((need, i) => (
            <Stack key={need} direction="horizontal" align="center" gap="tight">
              <Checkbox id={`need-${i}`} defaultChecked={i === 0} />
              <Label htmlFor={`need-${i}`}>{need}</Label>
            </Stack>
          ))}
        </fieldset>
      </Section>
      <Section title="Wrong, and disabled">
        <Stack gap="normal">
          <Stack direction="horizontal" align="center" gap="tight">
            <Checkbox id="wrong" aria-invalid />
            <Label htmlFor="wrong">A choice that is required</Label>
          </Stack>
          <Stack direction="horizontal" align="center" gap="tight">
            <Checkbox id="off" disabled defaultChecked />
            <Label htmlFor="off">Already set for you</Label>
          </Stack>
        </Stack>
      </Section>
    </>
  );
}
```

In `design/ui/app/catalog.ts`, in the `structures` pages, after the line whose `slug` is `"switch"`, add:

```ts
      { slug: "checkbox", title: "Checkbox", description: "One of several choices in a form that is sent later." },
```

In `design/ui/CHANGELOG.md`, under `## Unreleased`, add:

```markdown
- `Checkbox` (`@shpyrd/ui/components/checkbox`): one of several choices in a
  form that is sent later.
```

Run: `npm run dev --workspace design/ui`, open `http://localhost:4323/structures/checkbox`, check light and dark, and that Space toggles a focused checkbox (keyboard, which jsdom cannot press). Stop the server.

- [ ] **Step 6: The library's gates, and commit**

Run: `npm run lint --workspace design/ui && npm run typecheck --workspace design/ui && npm run test --workspace design/ui`
Expected: all pass.

```bash
git add design/ui/src/components/checkbox.tsx design/ui/src/components/checkbox.test.tsx design/ui/app/structures/checkbox/page.tsx design/ui/app/catalog.ts design/ui/CHANGELOG.md
git commit -m "feat(ui): Checkbox, one of several choices in a form sent later"
```

### Task 2: The forms as content

**Files:**
- Create: `content/site/contact.ts`
- Create: `content/site/contact.test.ts`

**Interfaces:**
- Produces, from `@shpyrd/content/site/contact`:
  - `type Choice = { value: string; label: string }`
  - `type ContactField = { name: string; label: string; type: "text" | "email" | "select" | "checkboxes" | "textarea"; required?: boolean; choices?: Choice[]; autoComplete?: string }`
  - `type ContactPage = { title: string; description: string; submit: string; fields: ContactField[]; beside: { heading: string; items: string[] }; after: { heading: string; items: string[] } }`
  - `sales: ContactPage`, `enterprise: ContactPage`
  - `privacy: { label: string; href: string }`
  - `technical: { text: string; link: string }`
  - `failed: string`
  - `thanks(email: string): string`

- [ ] **Step 1: Write the failing tests**

`content/site/contact.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { enterprise, privacy, sales, thanks } from "./contact";
import { pricing } from "./pricing";

describe("the contact pages", () => {
  it("ask what the spec says, in its order", () => {
    expect(sales.fields.map((f) => f.name)).toEqual(["firstName", "lastName", "email", "company", "interest", "message"]);
    expect(enterprise.fields.map((f) => f.name)).toEqual([
      "firstName", "lastName", "email", "company", "jobTitle", "size", "runsOn", "needs", "timeline", "message",
    ]);
  });

  it("give every select and every set of checkboxes its choices", () => {
    for (const f of [...sales.fields, ...enterprise.fields]) {
      if (f.type === "select" || f.type === "checkboxes") expect(f.choices?.length, f.name).toBeGreaterThan(1);
    }
  });

  it("say beside the enterprise form what the Enterprise plan adds", () => {
    expect(enterprise.beside.items).toEqual(pricing.enterprise.features);
  });

  it("link the privacy policy the footer links", () => {
    expect(privacy.href).toBe("https://legal.shpyrd.io/global/privacy-policy");
  });

  it("thank the person by the address they gave", () => {
    expect(thanks("ana@acme.com")).toContain("ana@acme.com");
  });

  it("write the name in lower case", () => {
    expect(JSON.stringify([sales, enterprise])).not.toMatch(/Shpyrd/);
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace content -- contact`
Expected: FAIL, `Failed to resolve import "./contact"`.

- [ ] **Step 3: Write the content**

`content/site/contact.ts`:

```ts
// The contact pages (apps/website/app/contact): what each asks, and what it
// says beside the form. The rules the page and the server hold the answers
// to are derived from these fields (apps/website/src/lib/contact.ts).
import { pricing } from './pricing'

export type Choice = { value: string; label: string }

export type ContactField = {
  name: string
  label: string
  type: 'text' | 'email' | 'select' | 'checkboxes' | 'textarea'
  required?: boolean
  choices?: Choice[]
  autoComplete?: string
}

export type ContactPage = {
  title: string
  description: string
  submit: string
  fields: ContactField[]
  beside: { heading: string; items: string[] }
  after: { heading: string; items: string[] }
}

const person: ContactField[] = [
  { name: 'firstName', label: 'First name', type: 'text', required: true, autoComplete: 'given-name' },
  { name: 'lastName', label: 'Last name', type: 'text', required: true, autoComplete: 'family-name' },
  { name: 'email', label: 'Work email', type: 'email', required: true, autoComplete: 'email' },
  { name: 'company', label: 'Company', type: 'text', required: true, autoComplete: 'organization' },
]

const replyFirst = 'A reply within one business day'

export const sales: ContactPage = {
  title: 'Contact sales',
  description: "Tell us about the apps your team would bring, and we'll set up a call.",
  submit: 'Request a call',
  fields: [
    ...person,
    {
      name: 'interest',
      label: 'What are you interested in?',
      type: 'select',
      required: true,
      choices: [
        { value: 'cloud', label: 'shpyrd cloud for my team' },
        { value: 'own-cloud', label: 'shpyrd in our own cloud' },
        { value: 'clients', label: 'Managing apps for clients' },
        { value: 'other', label: 'Something else' },
      ],
    },
    { name: 'message', label: 'Anything we should know?', type: 'textarea' },
  ],
  beside: {
    heading: 'What the call covers',
    items: [
      'The apps your team would bring',
      'Who should reach each of them',
      'Where they would run: shpyrd cloud, or your own cloud',
    ],
  },
  after: {
    heading: 'What follows',
    items: [replyFirst, 'A trial workspace on shpyrd cloud, or a plan for your own cloud'],
  },
}

export const enterprise: ContactPage = {
  title: 'Contact enterprise sales',
  description: 'shpyrd in your own cloud, on your terms, with everything shpyrd cloud has. Tell us what your company needs.',
  submit: 'Contact enterprise sales',
  fields: [
    ...person,
    { name: 'jobTitle', label: 'Job title', type: 'text', required: true, autoComplete: 'organization-title' },
    {
      name: 'size',
      label: 'Company size',
      type: 'select',
      required: true,
      choices: [
        { value: '1-49', label: '1–49' },
        { value: '50-249', label: '50–249' },
        { value: '250-999', label: '250–999' },
        { value: '1000+', label: '1000+' },
      ],
    },
    {
      name: 'runsOn',
      label: 'Where would shpyrd run?',
      type: 'select',
      required: true,
      choices: [
        { value: 'cloud', label: 'shpyrd cloud' },
        { value: 'own-cloud', label: 'Our own cloud (AWS, Oracle Cloud…)' },
        { value: 'on-premises', label: 'On-premises' },
        { value: 'unsure', label: 'Not sure yet' },
      ],
    },
    {
      name: 'needs',
      label: 'What do you need?',
      type: 'checkboxes',
      choices: [
        { value: 'sso', label: 'Single sign-on' },
        { value: 'review', label: 'Security or compliance review' },
        { value: 'sla', label: 'Support with an SLA' },
        { value: 'procurement', label: 'Invoicing and procurement' },
        { value: 'managed', label: 'Managed for our clients' },
      ],
    },
    {
      name: 'timeline',
      label: 'Timeline',
      type: 'select',
      required: true,
      choices: [
        { value: 'exploring', label: 'Exploring' },
        { value: 'quarter', label: 'This quarter' },
        { value: 'later', label: 'Next quarter or later' },
      ],
    },
    { name: 'message', label: 'Tell us about your setup', type: 'textarea' },
  ],
  beside: { heading: 'What Enterprise adds', items: pricing.enterprise.features },
  after: { heading: 'What follows', items: [replyFirst, 'A call about your setup', 'A license for a trial'] },
}

export const privacy = {
  label: 'Privacy Policy',
  href: 'https://legal.shpyrd.io/global/privacy-policy',
}

export const technical = { text: 'Technical question?', link: 'Ask in Discord' }

export const failed = 'The form could not be sent. Try again in a moment, or write to us in Discord.'

export const thanks = (email: string) => `Thanks, we'll reply to ${email} within one business day.`
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace content -- contact && npm run typecheck --workspace content`
Expected: PASS, 6 tests; type-check clean.

- [ ] **Step 5: Commit**

```bash
git add content/site/contact.ts content/site/contact.test.ts
git commit -m "feat(content): the sales and enterprise contact forms, as content"
```

### Task 3: The site's tests set up, and the rules of the forms

**Files:**
- Create: `apps/website/vitest.config.ts`
- Create: `apps/website/src/test-setup.ts`
- Modify: `apps/website/package.json` (devDependencies, by `npm install`)
- Create: `apps/website/src/lib/contact.ts`
- Create: `apps/website/src/lib/contact.test.ts`

**Interfaces:**
- Consumes: `sales`, `enterprise`, `ContactField` (Task 2).
- Produces, from `@/lib/contact`:
  - `type Kind = "sales" | "enterprise"`
  - `type Answers = Record<string, string | string[]>`
  - `formOf(kind: Kind): ContactPage`
  - `isKind(value: unknown): value is Kind`
  - `check(kind: Kind, answers: Answers): Record<string, string>`, field name → message; `{}` when all is well
  - `trap = "website"`, `minimumMs = 3000`
  - `isBot(submission: { website?: unknown; startedAt?: unknown }, now: number): boolean`

- [ ] **Step 1: Set up the tests**

Run: `npm install --save-dev --workspace apps/website @testing-library/react@^16.3.3 jsdom@^29.1.1`

`apps/website/vitest.config.ts`:

```ts
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// `@/` is src/, as in tsconfig.json. Components are tested in a browser
// that is made up (jsdom), with the file's `// @vitest-environment jsdom`.
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  esbuild: { jsx: "automatic" },
  test: { setupFiles: ["./src/test-setup.ts"] },
});
```

`apps/website/src/test-setup.ts`:

```ts
// What jsdom lacks and the library's components use, when a test runs in
// it; nothing in a test that runs in Node.
if (typeof window !== "undefined") {
  class Observer {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver ??= Observer as unknown as typeof ResizeObserver;
  window.matchMedia ??= (query: string) =>
    ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;
  Element.prototype.scrollIntoView ??= () => {};
  Element.prototype.hasPointerCapture ??= () => false;
}
```

Run: `npm run test --workspace apps/website`
Expected: the existing 16 tests still pass.

- [ ] **Step 2: Write the failing tests for the rules**

`apps/website/src/lib/contact.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { check, isBot, isKind, minimumMs } from "./contact";

const person = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme" };
const salesAnswers = { ...person, interest: "cloud" };
const enterpriseAnswers = { ...person, jobTitle: "CTO", size: "250-999", runsOn: "own-cloud", timeline: "quarter", needs: ["sso", "sla"] };

describe("check", () => {
  it("passes a complete form", () => {
    expect(check("sales", salesAnswers)).toEqual({});
    expect(check("enterprise", enterpriseAnswers)).toEqual({});
  });

  it("names every required field left empty", () => {
    expect(Object.keys(check("sales", {})).sort()).toEqual(["company", "email", "firstName", "interest", "lastName"]);
  });

  it("refuses an address that is not one", () => {
    expect(check("sales", { ...salesAnswers, email: "ana at acme" }).email).toBeTruthy();
  });

  it("refuses a value no select offers", () => {
    expect(check("sales", { ...salesAnswers, interest: "everything" }).interest).toBeTruthy();
  });

  it("refuses a checkbox the form does not have", () => {
    expect(check("enterprise", { ...enterpriseAnswers, needs: ["sso", "free-lunch"] }).needs).toBeTruthy();
  });

  it("refuses a field longer than it may be", () => {
    expect(check("sales", { ...salesAnswers, company: "x".repeat(101) }).company).toBeTruthy();
    expect(check("sales", { ...salesAnswers, message: "x".repeat(501) }).message).toBeTruthy();
    expect(check("sales", { ...salesAnswers, message: "x".repeat(500) })).toEqual({});
  });

  it("refuses a list where a word is expected", () => {
    expect(check("sales", { ...salesAnswers, company: ["Acme"] }).company).toBeTruthy();
  });
});

describe("isBot", () => {
  const start = 1_000_000;
  it("lets a person through", () => {
    expect(isBot({ website: "", startedAt: start }, start + minimumMs + 1)).toBe(false);
  });
  it("catches the hidden field filled", () => {
    expect(isBot({ website: "https://spam.example", startedAt: start }, start + 60_000)).toBe(true);
  });
  it("catches a form sent faster than a person can", () => {
    expect(isBot({ website: "", startedAt: start }, start + 500)).toBe(true);
  });
  it("catches a form with no start time", () => {
    expect(isBot({ website: "" }, start)).toBe(true);
  });
});

describe("isKind", () => {
  it("knows the two forms and nothing else", () => {
    expect(isKind("sales")).toBe(true);
    expect(isKind("enterprise")).toBe(true);
    expect(isKind("support")).toBe(false);
    expect(isKind(undefined)).toBe(false);
  });
});
```

- [ ] **Step 3: Run them to see them fail**

Run: `npm run test --workspace apps/website -- src/lib/contact`
Expected: FAIL, `Failed to resolve import "./contact"`.

- [ ] **Step 4: Write the rules**

`apps/website/src/lib/contact.ts`:

```ts
// The rules of the two contact forms, derived from their fields
// (@shpyrd/content/site/contact): the page shows them as the person types,
// the server holds every submission to them (app/api/contact/route.ts).
import { enterprise, sales, type ContactField, type ContactPage } from "@shpyrd/content/site/contact";

export type Kind = "sales" | "enterprise";
export type Answers = Record<string, string | string[]>;

export const isKind = (value: unknown): value is Kind => value === "sales" || value === "enterprise";
export const formOf = (kind: Kind): ContactPage => (kind === "sales" ? sales : enterprise);

const email = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const limit = (f: ContactField) => (f.type === "textarea" ? 500 : 100);

function fault(f: ContactField, value: string | string[] | undefined): string | undefined {
  if (f.type === "checkboxes") {
    if (value === undefined) return undefined;
    if (!Array.isArray(value)) return "Choose from the options.";
    const allowed = new Set(f.choices?.map((c) => c.value));
    return value.every((v) => allowed.has(v)) ? undefined : "Choose from the options.";
  }
  if (Array.isArray(value)) return "Write it as text.";
  const v = (value ?? "").trim();
  if (!v) return f.required ? "Fill this in." : undefined;
  if (v.length > limit(f)) return `Keep it under ${limit(f)} characters.`;
  if (f.type === "email" && !email.test(v)) return "Enter an address like you@company.com.";
  if (f.type === "select" && !f.choices?.some((c) => c.value === v)) return "Choose one of the options.";
  return undefined;
}

export function check(kind: Kind, answers: Answers): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const f of formOf(kind).fields) {
    const why = fault(f, answers[f.name]);
    if (why) errors[f.name] = why;
  }
  return errors;
}

// The hidden field people never see and bots fill in, and the least time
// a person takes between opening the page and sending the form.
export const trap = "website";
export const minimumMs = 3000;

export function isBot(submission: { website?: unknown; startedAt?: unknown }, now: number): boolean {
  if (typeof submission.website === "string" && submission.website !== "") return true;
  if (typeof submission.startedAt !== "number") return true;
  return now - submission.startedAt < minimumMs;
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `npm run test --workspace apps/website -- src/lib/contact`
Expected: PASS, 12 tests.

- [ ] **Step 6: Commit**

```bash
git add apps/website/vitest.config.ts apps/website/src/test-setup.ts apps/website/package.json package-lock.json apps/website/src/lib/contact.ts apps/website/src/lib/contact.test.ts
git commit -m "feat(website): the rules of the contact forms, and the site's tests set up for components"
```

### Task 4: The email, and the mailer

**Files:**
- Create: `apps/website/src/lib/contact-mail.ts`
- Create: `apps/website/src/lib/contact-mail.test.ts`
- Create: `apps/website/src/lib/mailer.ts`
- Create: `apps/website/src/lib/mailer.test.ts`
- Modify: `apps/website/package.json` (dependencies, by `npm install`)

**Interfaces:**
- Consumes: `Kind`, `Answers`, `formOf` (Task 3).
- Produces:
  - `contactMail(kind: Kind, answers: Answers, meta: { page: string; at: Date; country?: string }): { subject: string; text: string; html: string; replyTo: string }`
  - `type Outgoing = { subject: string; text: string; html: string; replyTo: string }`
  - `mailer(env?: Record<string, string | undefined>): ((mail: Outgoing) => Promise<void>) | null`; `null` when not configured.

- [ ] **Step 1: Install nodemailer**

Run: `npm install --workspace apps/website nodemailer && npm install --save-dev --workspace apps/website @types/nodemailer`

- [ ] **Step 2: Write the failing tests**

`apps/website/src/lib/contact-mail.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { contactMail } from "./contact-mail";

const meta = { page: "https://shpyrd.io/contact/enterprise", at: new Date("2026-10-09T12:00:00Z"), country: "BR" };
const enterpriseAnswers = {
  firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme", jobTitle: "CTO",
  size: "250-999", runsOn: "own-cloud", timeline: "quarter", needs: ["sso", "sla"], message: "Two clusters.",
};

describe("contactMail", () => {
  it("names the form, the person, the company and what they want in its subject", () => {
    expect(contactMail("enterprise", enterpriseAnswers, meta).subject).toBe(
      "[Enterprise] Ana Souza, Acme (250–999): Our own cloud (AWS, Oracle Cloud…)",
    );
    expect(
      contactMail("sales", { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme", interest: "cloud" }, meta).subject,
    ).toBe("[Sales] Ana Souza, Acme: shpyrd cloud for my team");
  });

  it("is answered by replying to the person", () => {
    expect(contactMail("enterprise", enterpriseAnswers, meta).replyTo).toBe("ana@acme.com");
  });

  it("lists every answer by its label, the choices by theirs, and where it came from", () => {
    const { text } = contactMail("enterprise", enterpriseAnswers, meta);
    expect(text).toContain("Job title: CTO");
    expect(text).toContain("What do you need?: Single sign-on, Support with an SLA");
    expect(text).toContain("Tell us about your setup: Two clusters.");
    expect(text).toContain("Page: https://shpyrd.io/contact/enterprise");
    expect(text).toContain("Sent: 2026-10-09T12:00:00.000Z");
    expect(text).toContain("Country: BR");
  });

  it("keeps the subject to one line whatever was typed", () => {
    const { subject } = contactMail("sales", { firstName: "Ana\r\nBcc: x@evil.test", lastName: "S", email: "a@b.co", company: "A", interest: "cloud" }, meta);
    expect(subject).not.toMatch(/[\r\n]/);
  });

  it("shows typed markup as text in the HTML", () => {
    const { html } = contactMail("sales", { firstName: "<b>Ana</b>", lastName: "S", email: "a@b.co", company: "A", interest: "cloud" }, meta);
    expect(html).toContain("&lt;b&gt;Ana&lt;/b&gt;");
    expect(html).not.toContain("<b>Ana</b>");
  });

  it("carries only the form's own fields", () => {
    const { text, html } = contactMail("sales", { firstName: "Ana", lastName: "S", email: "a@b.co", company: "A", interest: "cloud", injected: "x" } as never, meta);
    expect(text + html).not.toContain("injected");
  });
});
```

`apps/website/src/lib/mailer.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

const sendMail = vi.fn();
const createTransport = vi.fn(() => ({ sendMail }));
vi.mock("nodemailer", () => ({ default: { createTransport } }));

const { mailer } = await import("./mailer");

const env = {
  SMTP_HOST: "smtp.mailgun.org", SMTP_PORT: "587", SMTP_USER: "postmaster@mg.shpyrd.io", SMTP_PASS: "secret",
  CONTACT_FROM: "shpyrd website <website@mg.shpyrd.io>", CONTACT_TO: "sales@shpyrd.io",
};
const mail = { subject: "[Sales] Ana", text: "t", html: "<p>t</p>", replyTo: "ana@acme.com" };

describe("mailer", () => {
  beforeEach(() => {
    sendMail.mockReset();
    createTransport.mockClear();
  });

  it("is not there until every setting is", () => {
    expect(mailer({ ...env, SMTP_PASS: undefined })).toBeNull();
    expect(mailer({ ...env, CONTACT_TO: "" })).toBeNull();
  });

  it("speaks to the SMTP server it is given, over STARTTLS", () => {
    mailer(env);
    expect(createTransport).toHaveBeenCalledWith({
      host: "smtp.mailgun.org", port: 587, secure: false, requireTLS: true,
      auth: { user: "postmaster@mg.shpyrd.io", pass: "secret" },
    });
  });

  it("sends to the team only, from the site, answered by the person", async () => {
    await mailer(env)!({ ...mail, to: "victim@example.com" } as never);
    expect(sendMail).toHaveBeenCalledWith(expect.objectContaining({
      to: "sales@shpyrd.io", from: "shpyrd website <website@mg.shpyrd.io>", replyTo: "ana@acme.com",
    }));
  });
});
```

- [ ] **Step 3: Run them to see them fail**

Run: `npm run test --workspace apps/website -- src/lib/contact-mail src/lib/mailer`
Expected: FAIL, `Failed to resolve import "./contact-mail"` and `"./mailer"`.

- [ ] **Step 4: Write the email and the mailer**

`apps/website/src/lib/contact-mail.ts`:

```ts
// The email a contact form becomes: who wrote, what they answered, by the
// labels the form showed, and where it came from. Only the form's own
// fields are read; nothing typed reaches a header but the subject, which
// is kept to one line, and the HTML shows typed markup as text.
import type { ContactField } from "@shpyrd/content/site/contact";
import { formOf, type Answers, type Kind } from "./contact";

const oneLine = (s: string) => s.replace(/[\r\n]+/g, " ").trim();
const escape = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

function shown(f: ContactField, value: string | string[] | undefined): string {
  const label = (v: string) => f.choices?.find((c) => c.value === v)?.label ?? v;
  if (Array.isArray(value)) return value.map(label).join(", ");
  return value ? label(value) : "";
}

export function contactMail(kind: Kind, answers: Answers, meta: { page: string; at: Date; country?: string }) {
  const form = formOf(kind);
  const field = (name: string) => form.fields.find((f) => f.name === name)!;
  const a = (name: string) => oneLine(shown(field(name), answers[name]));
  const who = `${a("firstName")} ${a("lastName")}, ${a("company")}`;
  const subject =
    kind === "sales"
      ? `[Sales] ${who}: ${a("interest")}`
      : `[Enterprise] ${who} (${a("size")}): ${a("runsOn")}`;

  const rows: [string, string][] = form.fields
    .map((f): [string, string] => [f.label, shown(f, answers[f.name])])
    .filter(([, v]) => v !== "");
  rows.push(["Page", meta.page], ["Sent", meta.at.toISOString()]);
  if (meta.country) rows.push(["Country", meta.country]);

  return {
    subject: oneLine(subject),
    text: rows.map(([k, v]) => `${k}: ${v}`).join("\n"),
    html: `<table>${rows.map(([k, v]) => `<tr><th align="left">${escape(k)}</th><td>${escape(v)}</td></tr>`).join("")}</table>`,
    replyTo: oneLine(String(answers.email ?? "")),
  };
}
```

`apps/website/src/lib/mailer.ts`:

```ts
// Sends an email to the team through the SMTP server the site is given
// (Mailgun's): from CONTACT_FROM, to CONTACT_TO, whatever the mail carries.
// Without its settings there is no mailer, and the contact route says so.
import nodemailer from "nodemailer";

export type Outgoing = { subject: string; text: string; html: string; replyTo: string };

export function mailer(env: Record<string, string | undefined> = process.env) {
  const { SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, CONTACT_FROM, CONTACT_TO } = env;
  if (!SMTP_HOST || !SMTP_USER || !SMTP_PASS || !CONTACT_FROM || !CONTACT_TO) return null;
  const transport = nodemailer.createTransport({
    host: SMTP_HOST,
    port: Number(SMTP_PORT || 587),
    secure: false,
    requireTLS: true,
    auth: { user: SMTP_USER, pass: SMTP_PASS },
  });
  return async (mail: Outgoing) => {
    const { subject, text, html, replyTo } = mail;
    await transport.sendMail({ subject, text, html, replyTo, from: CONTACT_FROM, to: CONTACT_TO });
  };
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `npm run test --workspace apps/website -- src/lib/contact-mail src/lib/mailer`
Expected: PASS, 9 tests.

- [ ] **Step 6: Commit**

```bash
git add apps/website/src/lib/contact-mail.ts apps/website/src/lib/contact-mail.test.ts apps/website/src/lib/mailer.ts apps/website/src/lib/mailer.test.ts apps/website/package.json package-lock.json
git commit -m "feat(website): the email a contact form becomes, and the mailer that sends it"
```

### Task 5: The route

**Files:**
- Create: `apps/website/app/api/contact/route.ts`
- Create: `apps/website/app/api/contact/route.test.ts`

**Interfaces:**
- Consumes: `isKind`, `check`, `isBot` (Task 3); `contactMail` (Task 4); `mailer` (Task 4).
- Produces: `POST /api/contact`, body `{ kind: "sales" | "enterprise"; answers: Answers; website: string; startedAt: number }`; answers `200 { ok: true }`, `400 { errors }` or `400 { error }`, `503 { error }`, `502 { error }`. Task 7 posts to it.

- [ ] **Step 1: Write the failing tests**

`apps/website/app/api/contact/route.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

const send = vi.fn();
let configured = true;
vi.mock("@/lib/mailer", () => ({ mailer: () => (configured ? send : null) }));

const { POST } = await import("./route");

const answers = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme", interest: "cloud" };
const post = (body: unknown) =>
  POST(new Request("https://shpyrd.io/api/contact", {
    method: "POST",
    headers: { "content-type": "application/json", referer: "https://shpyrd.io/contact/sales", "x-vercel-ip-country": "BR" },
    body: typeof body === "string" ? body : JSON.stringify(body),
  }));
const human = { kind: "sales", answers, website: "", startedAt: Date.now() - 10_000 };

describe("POST /api/contact", () => {
  beforeEach(() => {
    send.mockReset();
    configured = true;
  });

  it("sends a good form to the team, answered by the person", async () => {
    const res = await post(human);
    expect(res.status).toBe(200);
    expect(send).toHaveBeenCalledOnce();
    expect(send.mock.calls[0][0]).toMatchObject({ replyTo: "ana@acme.com", subject: "[Sales] Ana Souza, Acme: shpyrd cloud for my team" });
    expect(send.mock.calls[0][0].text).toContain("Country: BR");
  });

  it("refuses a form with mistakes, field by field, and sends nothing", async () => {
    const res = await post({ ...human, answers: { ...answers, email: "nope" } });
    expect(res.status).toBe(400);
    expect((await res.json()).errors.email).toBeTruthy();
    expect(send).not.toHaveBeenCalled();
  });

  it("refuses what is not JSON, or no form it knows", async () => {
    expect((await post("not json")).status).toBe(400);
    expect((await post({ ...human, kind: "support" })).status).toBe(400);
    expect(send).not.toHaveBeenCalled();
  });

  it("answers a bot as if it had sent, and sends nothing", async () => {
    expect((await post({ ...human, website: "https://spam.example" })).status).toBe(200);
    expect((await post({ ...human, startedAt: Date.now() })).status).toBe(200);
    expect(send).not.toHaveBeenCalled();
  });

  it("says it cannot send where it is not configured", async () => {
    configured = false;
    expect((await post(human)).status).toBe(503);
  });

  it("says the mail did not go when the server refuses it, and logs why but not what", async () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    send.mockRejectedValueOnce(new Error("535 Authentication failed"));
    const res = await post(human);
    expect(res.status).toBe(502);
    expect(log).toHaveBeenCalledWith(expect.stringContaining("535 Authentication failed"));
    expect(JSON.stringify(log.mock.calls)).not.toContain("ana@acme.com");
    log.mockRestore();
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace apps/website -- app/api/contact`
Expected: FAIL, `Failed to resolve import "./route"`.

- [ ] **Step 3: Write the route**

`apps/website/app/api/contact/route.ts`:

```ts
// The contact forms' one route: checks a submission by the forms' rules
// (src/lib/contact.ts) and mails it to the team (src/lib/mailer.ts).
// The only part of the site that runs on request.
import { check, isBot, isKind, type Answers } from "@/lib/contact";
import { contactMail } from "@/lib/contact-mail";
import { mailer } from "@/lib/mailer";

export const runtime = "nodejs";

export async function POST(request: Request) {
  let body: { kind?: unknown; answers?: unknown; website?: unknown; startedAt?: unknown };
  try {
    body = await request.json();
  } catch {
    return Response.json({ error: "The form could not be read." }, { status: 400 });
  }
  if (!body || !isKind(body.kind)) {
    return Response.json({ error: "No such form." }, { status: 400 });
  }
  // A bot learns nothing: it is answered as a person would be.
  if (isBot(body, Date.now())) return Response.json({ ok: true });

  const answers = (body.answers && typeof body.answers === "object" ? body.answers : {}) as Answers;
  const errors = check(body.kind, answers);
  if (Object.keys(errors).length > 0) return Response.json({ errors }, { status: 400 });

  const send = mailer();
  if (!send) return Response.json({ error: "The form cannot be sent from here." }, { status: 503 });

  const mail = contactMail(body.kind, answers, {
    page: request.headers.get("referer") ?? "",
    at: new Date(),
    country: request.headers.get("x-vercel-ip-country") ?? undefined,
  });
  try {
    await send(mail);
  } catch (err) {
    // Why it failed, never what the person wrote.
    console.error(`contact: the ${body.kind} form was not mailed: ${err instanceof Error ? err.message : String(err)}`);
    return Response.json({ error: "The form could not be sent." }, { status: 502 });
  }
  return Response.json({ ok: true });
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace apps/website -- app/api/contact`
Expected: PASS, 6 tests.

- [ ] **Step 5: Commit**

```bash
git add apps/website/app/api/contact/route.ts apps/website/app/api/contact/route.test.ts
git commit -m "feat(website): POST /api/contact checks a form and mails it to the team"
```

### Task 6: The site stops being a static export

**Files:**
- Modify: `apps/website/next.config.ts`
- Modify: `apps/website/README.md`
- Modify: `docs/superpowers/plans/2026-09-29-design-content-apps.md` (the "Framework" row)

- [ ] **Step 1: Drop the export**

In `apps/website/next.config.ts`, replace the opening comment and `output: "export",` of `base`:

```ts
// Every page is rendered when the site is built; one route runs on request,
// the contact forms' (app/api/contact), as a function on Vercel. The
// redirects live in vercel.json, which Vercel answers before any page; the
// development server reads the same list.
const base: NextConfig = {
```

(the line `output: "export",` is removed; the rest of `base` stays). In `config(phase)`, the development branch no longer needs `output: undefined`: it becomes `return { ...base, redirects: async () => vercel.redirects as Redirect[] };`.

- [ ] **Step 2: Build, and read what runs on request**

Run: `npm run build --workspace apps/website 2>&1 | tee /tmp/website-build.log | tail -40`
Expected: the build passes; its route table marks `/api/contact` as `ƒ` (Dynamic) and every page as `○` (Static) or `●` (SSG); `/contact/...` pages do not exist yet.

- [ ] **Step 3: The README and the plan**

In `apps/website/README.md`, the line `npm --prefix apps/website run build      # static files in out/` becomes `npm --prefix apps/website run build      # every page rendered; /api/contact runs on request`; the layout line `vercel.json                  the redirects (/discord, /docs); a static export emits none` becomes `vercel.json                  the redirects (/discord, /docs), which Vercel answers first`; and add a section before `## License`:

```markdown
## The contact forms

`/contact/sales` and `/contact/enterprise` post to `app/api/contact`, the
one route that runs on request. It checks the answers by the forms' rules
(`src/lib/contact.ts`, from `content/site/contact.ts`) and mails them to the
team through Mailgun's SMTP. Its settings, in Vercel, for Production only:

| Variable | Example |
|---|---|
| `SMTP_HOST` | `smtp.mailgun.org` |
| `SMTP_PORT` | `587` |
| `SMTP_USER` | `postmaster@mg.shpyrd.io` |
| `SMTP_PASS` | Mailgun's SMTP password |
| `CONTACT_FROM` | `shpyrd website <website@mg.shpyrd.io>` |
| `CONTACT_TO` | `sales@shpyrd.io` |

Without them (a preview, the development server) the route answers `503`
and the page offers Discord: nothing is mailed.
```

In `docs/superpowers/plans/2026-09-29-design-content-apps.md`, the row

```markdown
| Framework | Next, for everything, compiled to static files; no Node at run time |
```

becomes

```markdown
| Framework | Next, for everything, compiled to static files; no Node at run time. The website, on Vercel, has one route on request: its contact forms ([their spec](../specs/2026-10-09-contact-pages-design.md)) |
```

- [ ] **Step 4: Commit**

```bash
git add apps/website/next.config.ts apps/website/README.md docs/superpowers/plans/2026-09-29-design-content-apps.md
git commit -m "feat(website): every page rendered at build, one route on request"
```

### Task 7: The two pages

**Files:**
- Create: `apps/website/src/components/contact-form.tsx`
- Create: `apps/website/src/components/contact-form.test.tsx`
- Create: `apps/website/src/components/contact.tsx`
- Create: `apps/website/app/contact/sales/page.tsx`
- Create: `apps/website/app/contact/enterprise/page.tsx`

**Interfaces:**
- Consumes: `Checkbox` (Task 1); `sales`, `enterprise`, `privacy`, `technical`, `failed`, `thanks` (Task 2); `check`, `formOf`, `trap`, `Kind`, `Answers` (Task 3); `POST /api/contact` (Task 5).
- Produces: `ContactForm({ kind }: { kind: Kind })` (client), `Contact({ kind }: { kind: Kind })` (the page's layout), and the pages `/contact/sales`, `/contact/enterprise`. Task 8 links to them.

- [ ] **Step 1: Write the failing tests**

`apps/website/src/components/contact-form.test.tsx`:

```tsx
// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ContactForm } from "./contact-form";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function fill(container: HTMLElement) {
  for (const [label, value] of [["First name", "Ana"], ["Last name", "Souza"], ["Work email", "ana@acme.com"], ["Company", "Acme"]]) {
    fireEvent.change(screen.getByLabelText(label, { exact: false }), { target: { value } });
  }
  // Radix's Select keeps a native select in a form, which is what a test
  // (and the browser's autofill) can change.
  fireEvent.change(container.querySelector("select[name=interest]")!, { target: { value: "cloud" } });
}

describe("ContactForm", () => {
  it("shows the sales form's fields", () => {
    render(<ContactForm kind="sales" />);
    for (const label of ["First name", "Last name", "Work email", "Company", "Anything we should know?"]) {
      expect(screen.getByLabelText(label, { exact: false })).toBeTruthy();
    }
    expect(screen.getByRole("button", { name: "Request a call" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Privacy Policy" }).getAttribute("href")).toBe("https://legal.shpyrd.io/global/privacy-policy");
  });

  it("says what is missing, under each field, and sends nothing", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    render(<ContactForm kind="sales" />);
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    expect(screen.getAllByText("Fill this in.").length).toBeGreaterThanOrEqual(4);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("sends a complete form and thanks the person by their address", async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    const { container } = render(<ContactForm kind="sales" />);
    fill(container);
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    await waitFor(() => expect(screen.getByText(/we'll reply to ana@acme.com/)).toBeTruthy());
    const [url, init] = fetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/contact");
    const body = JSON.parse(String(init.body));
    expect(body).toMatchObject({ kind: "sales", website: "", answers: { firstName: "Ana", interest: "cloud" } });
    expect(typeof body.startedAt).toBe("number");
  });

  it("keeps what was typed and offers Discord when it cannot send", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "x" }), { status: 502 })));
    const { container } = render(<ContactForm kind="sales" />);
    fill(container);
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    await waitFor(() => expect(screen.getByText(/could not be sent/)).toBeTruthy());
    expect((screen.getByLabelText("Company", { exact: false }) as HTMLInputElement).value).toBe("Acme");
    expect(screen.getByRole("link", { name: /Discord/ }).getAttribute("href")).toBe("/discord");
  });

  it("shows the enterprise form's choices as checkboxes", () => {
    render(<ContactForm kind="enterprise" />);
    expect(screen.getByRole("checkbox", { name: "Single sign-on" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Contact enterprise sales" })).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace apps/website -- src/components/contact-form`
Expected: FAIL, `Failed to resolve import "./contact-form"`.

- [ ] **Step 3: Write the form**

`apps/website/src/components/contact-form.tsx`:

```tsx
"use client";

// A contact form: its fields from the content, checked by the forms' rules
// as it is sent, posted to /api/contact. Sent, it gives way to the thanks;
// not sent, it keeps what was typed and offers Discord.
import * as React from "react";
import { Button } from "@shpyrd/ui/components/button";
import { Checkbox } from "@shpyrd/ui/components/checkbox";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { failed, privacy, technical, thanks, type ContactField } from "@shpyrd/content/site/contact";
import { discord } from "@shpyrd/content/site/offer";
import { check, formOf, trap, type Answers, type Kind } from "@/lib/contact";

type Status = "idle" | "sending" | "sent" | "failed";

export function ContactForm({ kind }: { kind: Kind }) {
  const form = formOf(kind);
  const [answers, setAnswers] = React.useState<Answers>({});
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const [status, setStatus] = React.useState<Status>("idle");
  const [website, setWebsite] = React.useState("");
  const startedAt = React.useRef(0);
  React.useEffect(() => {
    startedAt.current = Date.now();
  }, []);

  const set = (name: string, value: string | string[]) => setAnswers((a) => ({ ...a, [name]: value }));
  const text = (name: string) => (typeof answers[name] === "string" ? (answers[name] as string) : "");
  const list = (name: string) => (Array.isArray(answers[name]) ? (answers[name] as string[]) : []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const found = check(kind, answers);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    setStatus("sending");
    try {
      const res = await fetch("/api/contact", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ kind, answers, [trap]: website, startedAt: startedAt.current }),
      });
      if (res.ok) {
        setStatus("sent");
        (window as unknown as { dataLayer?: unknown[] }).dataLayer?.push({ event: "contact_submitted", form: kind });
        return;
      }
      if (res.status === 400) {
        const body = (await res.json().catch(() => ({}))) as { errors?: Record<string, string> };
        if (body.errors) {
          setErrors(body.errors);
          setStatus("idle");
          return;
        }
      }
      setStatus("failed");
    } catch {
      setStatus("failed");
    }
  }

  if (status === "sent") {
    return (
      <p role="status" className="text-lg">
        {thanks(text("email"))}
      </p>
    );
  }

  const control = (f: ContactField) => {
    const id = `contact-${f.name}`;
    switch (f.type) {
      case "select":
        return (
          <Field key={f.name} id={id} label={f.label} required={f.required} error={errors[f.name]}>
            <Select name={f.name} value={text(f.name)} onValueChange={(v) => set(f.name, v)}>
              <SelectTrigger id={id} aria-invalid={errors[f.name] ? true : undefined} className="w-full">
                <SelectValue placeholder="Choose…" />
              </SelectTrigger>
              <SelectContent>
                {f.choices?.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        );
      case "checkboxes":
        return (
          <fieldset key={f.name} className="grid gap-3 sm:col-span-2">
            <legend className="mb-1 text-sm font-medium">{f.label}</legend>
            {f.choices?.map((c) => {
              const cid = `${id}-${c.value}`;
              const chosen = list(f.name);
              return (
                <div key={c.value} className="flex items-center gap-2.5">
                  <Checkbox
                    id={cid}
                    checked={chosen.includes(c.value)}
                    onCheckedChange={(on) =>
                      set(f.name, on === true ? [...chosen, c.value] : chosen.filter((v) => v !== c.value))
                    }
                  />
                  <Label htmlFor={cid}>{c.label}</Label>
                </div>
              );
            })}
            {errors[f.name] && <p className="text-xs text-destructive">{errors[f.name]}</p>}
          </fieldset>
        );
      case "textarea":
        return (
          <Field key={f.name} label={f.label} error={errors[f.name]} className="sm:col-span-2">
            <Textarea id={id} name={f.name} rows={4} value={text(f.name)} onChange={(e) => set(f.name, e.target.value)} />
          </Field>
        );
      default:
        return (
          <Field key={f.name} label={f.label} required={f.required} error={errors[f.name]}>
            <Input
              id={id}
              name={f.name}
              type={f.type}
              autoComplete={f.autoComplete}
              value={text(f.name)}
              onChange={(e) => set(f.name, e.target.value)}
            />
          </Field>
        );
    }
  };

  return (
    <form noValidate onSubmit={submit} className="grid gap-5 sm:grid-cols-2">
      {status === "failed" && (
        <p role="alert" className="text-sm text-destructive sm:col-span-2">
          {failed} <a href={discord.href} className="underline underline-offset-4">Discord</a>
        </p>
      )}
      {form.fields.map(control)}
      {/* People never see this field; bots fill it in. */}
      <input
        type="text"
        name={trap}
        tabIndex={-1}
        autoComplete="off"
        aria-hidden
        value={website}
        onChange={(e) => setWebsite(e.target.value)}
        className="absolute -left-[9999px] size-px opacity-0"
      />
      <div className="grid gap-3 sm:col-span-2">
        <Button type="submit" disabled={status === "sending"} className="justify-self-start">
          {form.submit}
        </Button>
        <p className="text-xs text-muted-foreground">
          By submitting, you agree to our{" "}
          <a href={privacy.href} className="underline underline-offset-4">
            {privacy.label}
          </a>
          .
        </p>
        <p className="text-xs text-muted-foreground">
          {technical.text}{" "}
          <a href={discord.href} className="underline underline-offset-4">
            {technical.link}
          </a>
        </p>
      </div>
    </form>
  );
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace apps/website -- src/components/contact-form`
Expected: PASS, 5 tests. If the Select's native element is absent in jsdom, rule in the ledger on how the test sets the value (for example through the Select's keyboard), not by adding a test-only prop.

- [ ] **Step 5: The page layout and the two pages**

`apps/website/src/components/contact.tsx`:

```tsx
import { ChecklistItems } from "@shpyrd/ui/components/checklist";
import { Hero } from "@shpyrd/ui/components/hero";
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { ContactForm } from "@/components/contact-form";
import { formOf, type Kind } from "@/lib/contact";

// A contact page: its heading, the form, and beside it what the call covers
// and what follows.
export function Contact({ kind }: { kind: Kind }) {
  const page = formOf(kind);
  return (
    <PageLayoutContent width="xlarge" padding="normal" className="grid grid-cols-1 content-start gap-12 py-8">
      <Hero variant="page" heading={page.title} description={page.description} />
      <div className="grid gap-12 @4xl/page-layout:grid-cols-[3fr_2fr]">
        <ContactForm kind={kind} />
        <aside className="grid content-start gap-8">
          {[page.beside, page.after].map((part) => (
            <section key={part.heading} className="grid gap-3">
              <h2 className="font-medium">{part.heading}</h2>
              <ChecklistItems items={part.items} />
            </section>
          ))}
        </aside>
      </div>
    </PageLayoutContent>
  );
}
```

`apps/website/app/contact/sales/page.tsx`:

```tsx
import { sales } from "@shpyrd/content/site/contact";
import { Contact } from "@/components/contact";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: sales.title,
  description: sales.description,
  ...shared(`${sales.title} · shpyrd`, sales.description),
};

export default function Page() {
  return <Contact kind="sales" />;
}
```

`apps/website/app/contact/enterprise/page.tsx`:

```tsx
import { enterprise } from "@shpyrd/content/site/contact";
import { Contact } from "@/components/contact";
import { shared } from "@/lib/metadata";

export const metadata = {
  title: enterprise.title,
  description: enterprise.description,
  ...shared(`${enterprise.title} · shpyrd`, enterprise.description),
};

export default function Page() {
  return <Contact kind="enterprise" />;
}
```

- [ ] **Step 6: Look at them**

Run: `npm run dev --workspace apps/website` and open `http://localhost:<its port>/contact/sales` and `/contact/enterprise`: light and dark, at 1440 and 390 pixels wide; send each empty (the errors), then complete (without the variables the route answers `503`: the page shows the failure and Discord, what was typed kept). Stop the server.

- [ ] **Step 7: The site's gates, and commit**

Run: `npm run lint --workspace apps/website && npm run typecheck --workspace apps/website && npm run test --workspace apps/website && npm run build --workspace apps/website`
Expected: all pass; the build lists `/contact/sales` and `/contact/enterprise` as `○` (Static) and `/api/contact` as `ƒ`.

```bash
git add apps/website/src/components/contact-form.tsx apps/website/src/components/contact-form.test.tsx apps/website/src/components/contact.tsx apps/website/app/contact
git commit -m "feat(website): the sales and enterprise contact pages"
```

### Task 8: "Contact us", to the pages

**Files:**
- Modify: `content/site/offer.ts` (`contact`, and a new `enterpriseContact`)
- Modify: `content/site/pricing.ts` (the Enterprise plan's action label)
- Modify: `content/site/pricing.test.ts` (its expectation)
- Modify: `apps/website/src/components/pricing.tsx` (the Enterprise button's link; the line under the FAQ)
- Create: `apps/website/src/lib/contact-links.test.ts`

**Interfaces:**
- Consumes: the pages of Task 7.
- Produces: `contact = { href: "/contact/sales", label: "Contact us" }`, `enterpriseContact = { href: "/contact/enterprise", label: "Contact us" }` from `@shpyrd/content/site/offer`.

- [ ] **Step 1: Write the failing test**

`apps/website/src/lib/contact-links.test.ts`:

```ts
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { contact, enterpriseContact } from "@shpyrd/content/site/offer";
import { pricing } from "@shpyrd/content/site/pricing";

const app = fileURLToPath(new URL("../../app", import.meta.url));
const pricingSource = fileURLToPath(new URL("../components/pricing.tsx", import.meta.url));

describe("the contact links", () => {
  it("lead to the contact pages, which exist", () => {
    expect(contact.href).toBe("/contact/sales");
    expect(enterpriseContact.href).toBe("/contact/enterprise");
    for (const href of [contact.href, enterpriseContact.href]) {
      expect(existsSync(`${app}${href}/page.tsx`), href).toBe(true);
    }
  });

  it("read Contact us, and nowhere Talk to us", () => {
    expect(pricing.enterprise.action.label).toBe("Contact us");
    expect(readFileSync(pricingSource, "utf8")).not.toMatch(/Talk to us/);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npm run test --workspace apps/website -- src/lib/contact-links`
Expected: FAIL: `enterpriseContact` is undefined, and `contact.href` is `/discord`.

- [ ] **Step 3: Change the links and the words**

In `content/site/offer.ts`, replace the `contact` export with:

```ts
// Where "Contact us" leads: the sales form, and the enterprise form for the
// Enterprise plan (apps/website/app/contact).
export const contact = {
  href: '/contact/sales',
  label: 'Contact us',
}

export const enterpriseContact = {
  href: '/contact/enterprise',
  label: 'Contact us',
}
```

Run `grep -rn "contact\.\(note\|label\)" apps/website content` first: if anything reads `contact.note`, move that sentence where it is used rather than dropping it.

In `content/site/pricing.ts`, the Enterprise plan's `action: { label: 'Talk to us', kind: 'contact' as const }` becomes `action: { label: 'Contact us', kind: 'contact' as const }`. In `content/site/pricing.test.ts`, the expectation `{ label: "Talk to us", kind: "contact" }` becomes `{ label: "Contact us", kind: "contact" }`.

In `apps/website/src/components/pricing.tsx`: import `enterpriseContact` beside `contact` from `@shpyrd/content/site/offer`; the Enterprise button's `<a href={contact.href}>{enterprise.action.label}</a>` becomes `<a href={enterpriseContact.href}>{enterprise.action.label}</a>`; the FAQ's `Talk to us` becomes `Contact us`.

Run: `grep -rn "Talk to us" content apps/website/src apps/website/app`
Expected: nothing.

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace apps/website -- src/lib/contact-links && npm run test --workspace content`
Expected: PASS.

- [ ] **Step 5: The gates, a look, and commit**

Run: `npm run lint --workspace apps/website && npm run typecheck --workspace apps/website && npm run test --workspace apps/website && npm run build --workspace apps/website`
Expected: all pass. In the development server: the pricing page's Enterprise "Contact us" opens `/contact/enterprise`, its plans' contact button and the FAQ's "Contact us" open `/contact/sales`, and a Solutions page's contact link opens `/contact/sales`.

```bash
git add content/site/offer.ts content/site/pricing.ts content/site/pricing.test.ts apps/website/src/components/pricing.tsx apps/website/src/lib/contact-links.test.ts
git commit -m "feat(website): Contact us leads to the contact pages"
```

### Task 9: The pull request

- [ ] **Step 1: Push and open it**

```bash
git push -u origin feat/contact-pages
gh pr create --base main --title "Website: contact pages for sales and enterprise, mailed through Mailgun" --body "Implements docs/superpowers/specs/2026-10-09-contact-pages-design.md (plan: docs/superpowers/plans/2026-10-09-contact-pages.md). Before merging: Vercel's output directory must not be set to out; the six variables (apps/website/README.md, The contact forms) set for Production."
```

Expected: the pull request's checks pass (`website` and `ci`).

- [ ] **Step 2: A real send, once the owner has given the credentials**

With the six variables in `apps/website/.env.local` (never committed), run the development server, send each form to a test inbox, and check the email: subject, `Reply-To`, every answer, the page and the time. Remove `.env.local` afterwards.
