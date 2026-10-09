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
      contactMail("sales", { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme", message: "Six apps." }, meta).subject,
    ).toBe("[Sales] Ana Souza, Acme");
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
    const { subject } = contactMail("sales", { firstName: "Ana\r\nBcc: x@evil.test", lastName: "S", email: "a@b.co", company: "A", message: "m" }, meta);
    expect(subject).not.toMatch(/[\r\n]/);
  });

  it("shows typed markup as text in the HTML", () => {
    const { html } = contactMail("sales", { firstName: "<b>Ana</b>", lastName: "S", email: "a@b.co", company: "A", message: "m" }, meta);
    expect(html).toContain("&lt;b&gt;Ana&lt;/b&gt;");
    expect(html).not.toContain("<b>Ana</b>");
  });

  it("carries only the form's own fields", () => {
    const { text, html } = contactMail("sales", { firstName: "Ana", lastName: "S", email: "a@b.co", company: "A", message: "m", injected: "x" } as never, meta);
    expect(text + html).not.toContain("injected");
  });
});
