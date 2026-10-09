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
      // Gives up well before a Vercel function is stopped, so a server that
      // does not answer is a 502 with its reason in the log.
      connectionTimeout: 10_000, greetingTimeout: 10_000, socketTimeout: 10_000,
    });
  });

  it("sends to the team only, from the site, answered by the person", async () => {
    await mailer(env)!({ ...mail, to: "victim@example.com" } as never);
    expect(sendMail).toHaveBeenCalledWith(expect.objectContaining({
      to: "sales@shpyrd.io", from: "shpyrd website <website@mg.shpyrd.io>", replyTo: "ana@acme.com",
    }));
  });
});
