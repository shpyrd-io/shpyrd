import { describe, expect, it } from "vitest";
import { contactMail } from "./contact-mail";

const at = new Date("2026-10-09T18:40:00Z");
const sales = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", message: "Six apps to move.\nCan we keep Google sign-in?" };
const enterprise = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme", jobTitle: "CTO", size: "201-500", message: "Two clusters." };
const salesMeta = { page: "https://www.shpyrd.io/contact/sales", at, country: "BR" };

describe("contactMail", () => {
  it("names the form, the person and the company in its subject, as a reply will show it", () => {
    expect(contactMail("sales", sales, salesMeta).subject).toBe("Contact sales: Ana Souza, acme.com");
    expect(contactMail("enterprise", enterprise, { page: "https://www.shpyrd.io/contact/enterprise", at }).subject).toBe(
      "Contact enterprise sales: Ana Souza, Acme (201–500 employees)",
    );
  });

  it("is answered by replying to the person", () => {
    expect(contactMail("sales", sales, salesMeta).replyTo).toBe("ana@acme.com");
  });

  it("reads as the person's own message: who wrote, what they asked, their details, where and when", () => {
    const { text } = contactMail("enterprise", enterprise, { page: "https://www.shpyrd.io/contact/enterprise", at });
    expect(text).toBe(
      [
        "Ana Souza (ana@acme.com) wrote to the shpyrd enterprise sales team:",
        "",
        "> Two clusters.",
        "",
        "Name: Ana Souza",
        "Email: ana@acme.com",
        "Company: Acme",
        "Job title: CTO",
        "Company size: 201–500 employees",
        "",
        "Sent from shpyrd.io/contact/enterprise on 9 October 2026 at 18:40 UTC.",
      ].join("\n"),
    );
  });

  it("quotes every line of a message of several", () => {
    expect(contactMail("sales", sales, salesMeta).text).toContain("> Six apps to move.\n> Can we keep Google sign-in?");
  });

  it("keeps what it knows of the visitor's whereabouts to itself", () => {
    const { text, html } = contactMail("sales", sales, salesMeta);
    expect(text + html).not.toMatch(/Country|\bBR\b/);
  });

  it("keeps the subject to one line whatever was typed", () => {
    const { subject } = contactMail("sales", { ...sales, firstName: "Ana\r\nBcc: x@evil.test" }, salesMeta);
    expect(subject).not.toMatch(/[\r\n]/);
  });

  it("shows typed markup as text in the HTML", () => {
    const { html } = contactMail("sales", { ...sales, firstName: "<b>Ana</b>", message: "<script>x</script>" }, salesMeta);
    expect(html).toContain("&lt;b&gt;Ana&lt;/b&gt;");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("<b>Ana</b>");
    expect(html).not.toContain("<script>");
  });

  it("carries only the form's own fields", () => {
    const { text, html } = contactMail("sales", { ...sales, injected: "x" } as never, salesMeta);
    expect(text + html).not.toContain("injected");
  });
});
