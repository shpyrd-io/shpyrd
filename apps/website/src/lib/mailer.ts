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
    // Gives up well before a Vercel function is stopped, so a server that
    // does not answer is a 502 with its reason in the log.
    connectionTimeout: 10_000,
    greetingTimeout: 10_000,
    socketTimeout: 10_000,
  });
  return async (mail: Outgoing) => {
    const { subject, text, html, replyTo } = mail;
    await transport.sendMail({ subject, text, html, replyTo, from: CONTACT_FROM, to: CONTACT_TO });
  };
}
