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
  if (typeof value !== "string" || !value) return "";
  return f.choices?.find((c) => c.value === value)?.label ?? value;
}

export function contactMail(kind: Kind, answers: Answers, meta: { page: string; at: Date; country?: string }) {
  const form = formOf(kind);
  const field = (name: string) => form.fields.find((f) => f.name === name)!;
  const a = (name: string) => oneLine(shown(field(name), answers[name]));
  const name = `${a("firstName")} ${a("lastName")}`;
  // The sales form asks no company: its email's domain says which.
  const domain = oneLine(String(answers.email ?? "")).split("@")[1] ?? "";
  const subject =
    kind === "sales"
      ? `[Sales] ${name}, ${domain}`
      : `[Enterprise] ${name}, ${a("company")} (${a("size")})`;

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
