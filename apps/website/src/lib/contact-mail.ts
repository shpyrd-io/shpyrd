// The email a contact form becomes. The team answers it with Reply, which
// goes to the person (Reply-To); the reply then quotes it back to them, so
// it reads as their own message: who wrote, what they asked, their details,
// and where and when it was sent. Only the form's own fields are read;
// nothing typed reaches a header but the subject, kept to one line, and the
// HTML shows typed markup as text.
import type { ContactField } from "@shpyrd/content/site/contact";
import { formOf, type Answers, type Kind } from "./contact";

const oneLine = (s: string) => s.replace(/[\r\n]+/g, " ").trim();
const escape = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

function shown(f: ContactField | undefined, value: string | string[] | undefined): string {
  if (!f || typeof value !== "string" || !value) return "";
  return f.choices?.find((c) => c.value === value)?.label ?? value;
}

// Where it was sent from, without the scheme: shpyrd.io/contact/sales.
const where = (page: string) => page.replace(/^https?:\/\/(www\.)?/, "").replace(/\/$/, "");
const when = (at: Date) =>
  `${at.toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" })} at ${at.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone: "UTC" })} UTC`;

export function contactMail(kind: Kind, answers: Answers, meta: { page: string; at: Date; country?: string }) {
  const form = formOf(kind);
  const field = (name: string) => form.fields.find((f) => f.name === name);
  const a = (name: string) => oneLine(shown(field(name), answers[name]));
  const name = `${a("firstName")} ${a("lastName")}`;
  const email = a("email");
  // The sales form asks no company: its email's domain says which.
  const domain = email.split("@")[1] ?? "";
  const subject =
    kind === "sales"
      ? `${form.title}: ${name}, ${domain}`
      : `${form.title}: ${name}, ${a("company")} (${a("size")})`;
  const message = shown(field("message"), answers.message).trim();

  // The person's details, by the labels the form showed; the message is
  // quoted on its own above them.
  const details: [string, string][] = [["Name", name], ["Email", email]];
  for (const f of form.fields) {
    if (["firstName", "lastName", "email", "message"].includes(f.name)) continue;
    const v = a(f.name);
    if (v) details.push([f.label, v]);
  }
  const intro = `${name} (${email}) wrote to the shpyrd ${kind === "sales" ? "sales" : "enterprise sales"} team:`;
  const footer = `Sent from ${where(meta.page) || "shpyrd.io"} on ${when(meta.at)}.`;

  const text = [
    intro,
    "",
    ...message.split("\n").map((l) => `> ${l}`),
    "",
    ...details.map(([k, v]) => `${k}: ${v}`),
    "",
    footer,
  ].join("\n");

  const font = "-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif";
  const html = `<div style="font-family:${font};font-size:15px;line-height:1.55;color:#1a1a1a;max-width:560px">
<p style="margin:0 0 16px">${escape(intro)}</p>
<blockquote style="margin:0 0 24px;padding:12px 16px;border-left:3px solid #ff4f00;background:#fff4ee;border-radius:0 6px 6px 0;white-space:pre-wrap">${escape(message)}</blockquote>
<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:14px">${details
    .map(
      ([k, v]) =>
        `<tr><td style="padding:3px 20px 3px 0;color:#6b6b6b;vertical-align:top">${escape(k)}</td><td style="padding:3px 0">${
          k === "Email" ? `<a href="mailto:${escape(v)}" style="color:#1a1a1a">${escape(v)}</a>` : escape(v)
        }</td></tr>`,
    )
    .join("")}</table>
<p style="margin:24px 0 0;font-size:12px;color:#8a8a8a">${escape(footer)}</p>
</div>`;

  return { subject: oneLine(subject), text, html, replyTo: email };
}
