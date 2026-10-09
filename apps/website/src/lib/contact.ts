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

// A submission comes from anywhere: a value is checked for what it is, not
// for what the page would have sent.
function fault(f: ContactField, value: unknown): string | undefined {
  if (f.type === "checkboxes") {
    if (value === undefined || value === null) return undefined;
    if (!Array.isArray(value)) return "Choose from the options.";
    const allowed = new Set(f.choices?.map((c) => c.value));
    return value.every((v) => typeof v === "string" && allowed.has(v)) ? undefined : "Choose from the options.";
  }
  if (value !== undefined && value !== null && typeof value !== "string") return "Write it as text.";
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
// a person takes to fill the form in. The page measures that time itself
// (`elapsedMs`): a clock of the person's that is ahead or behind the
// server's cannot make them a bot.
// Named so no browser or password manager fills it in for a person.
export const trap = "hp_check";
export const minimumMs = 3000;

export function isBot(submission: Record<string, unknown>): boolean {
  const hidden = submission[trap];
  if (typeof hidden === "string" && hidden !== "") return true;
  if (typeof submission.elapsedMs !== "number" || !Number.isFinite(submission.elapsedMs)) return true;
  return submission.elapsedMs < minimumMs;
}
