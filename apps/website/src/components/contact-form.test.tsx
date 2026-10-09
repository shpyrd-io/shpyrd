// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { ContactForm, wideFields } from "./contact-form";
import { enterprise, sales } from "@shpyrd/content/site/contact";
import { trap } from "@/lib/contact";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function fill() {
  for (const [label, value] of [["First name", "Ana"], ["Last name", "Souza"], ["Company email", "ana@acme.com"], ["How can we help you?", "Six apps to move."]]) {
    fireEvent.change(screen.getByLabelText(label, { exact: false }), { target: { value } });
  }
}

describe("ContactForm", () => {
  it("shows the sales form's fields", () => {
    render(<ContactForm kind="sales" />);
    for (const label of ["First name", "Last name", "Company email", "How can we help you?"]) {
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
    expect(screen.getAllByText("Fill this in.").length).toBe(4);
    expect(screen.queryByLabelText(/^Company\*?$/)).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("sends a complete form and thanks the person by their address", async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    render(<ContactForm kind="sales" />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    await waitFor(() => expect(screen.getByText(/we'll reply to ana@acme.com/)).toBeTruthy());
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/contact");
    const body = JSON.parse(String(init.body));
    expect(body).toMatchObject({ kind: "sales", [trap]: "", answers: { firstName: "Ana", message: "Six apps to move." } });
    expect(body.elapsedMs).toBeGreaterThanOrEqual(0);
    expect(body).not.toHaveProperty("startedAt");
  });

  it("keeps what was typed and offers Discord when it cannot send", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "x" }), { status: 502 })));
    render(<ContactForm kind="sales" />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    await waitFor(() => expect(screen.getByText(/could not be sent/)).toBeTruthy());
    expect((screen.getByLabelText("How can we help you?", { exact: false }) as HTMLTextAreaElement).value).toBe("Six apps to move.");
    expect(screen.getAllByRole("link", { name: /Discord/ })[0].getAttribute("href")).toBe("/discord");
  });

  it("asks the enterprise form's question, with no checkboxes", () => {
    render(<ContactForm kind="enterprise" />);
    expect(screen.getByLabelText("How can we help you?", { exact: false })).toBeTruthy();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByRole("button", { name: "Contact enterprise sales" })).toBeTruthy();
  });

  it("moves to the first field that needs fixing, so its error is heard", () => {
    vi.stubGlobal("fetch", vi.fn());
    render(<ContactForm kind="sales" />);
    fireEvent.click(screen.getByRole("button", { name: "Request a call" }));
    expect(document.activeElement).toBe(screen.getByLabelText("First name", { exact: false }));
  });

  it("has no menu to choose from on the sales form", () => {
    render(<ContactForm kind="sales" />);
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("ties a select to its error", () => {
    vi.stubGlobal("fetch", vi.fn());
    render(<ContactForm kind="enterprise" />);
    fireEvent.click(screen.getByRole("button", { name: "Contact enterprise sales" }));
    const trigger = screen.getAllByRole("combobox")[0];
    const described = (trigger.getAttribute("aria-describedby") ?? "").split(" ").map((id) => document.getElementById(id)?.textContent);
    expect(described).toContain("Fill this in.");
  });

  it("ties a field to the error the server gives it", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ errors: { size: "Choose one of the options." } }), { status: 400 })));
    const { container } = render(<ContactForm kind="enterprise" />);
    for (const [label, value] of [["First name", "Ana"], ["Last name", "Souza"], ["Company email", "ana@acme.com"], ["Company", "Acme"], ["Job title", "CTO"], ["How can we help you?", "Two clusters."]]) {
      // The exact label (with its required mark): "Company" is also in "Company size".
      fireEvent.change(screen.getByLabelText(new RegExp(`^${label.replace("?", "\\?")}\\*?$`)), { target: { value } });
    }
    fireEvent.change(container.querySelector("select[name=size]")!, { target: { value: "51-200" } });
    fireEvent.click(screen.getByRole("button", { name: "Contact enterprise sales" }));
    await waitFor(() => expect(screen.getByText("Choose one of the options.")).toBeTruthy());
    const trigger = screen.getByRole("combobox");
    expect(document.getElementById(trigger.getAttribute("aria-describedby") ?? "")?.textContent).toBe("Choose one of the options.");
    expect(document.activeElement).toBe(trigger);
  });

  // As the server sends it, before the page's script runs: a click then must
  // not send the answers as a GET, into the address bar and the logs.
  it("cannot be sent before the page is ready, and never by GET", () => {
    const html = renderToString(<ContactForm kind="sales" />);
    expect(html).toMatch(/<form[^>]*method="post"/);
    expect(html).toMatch(/<button[^>]*type="submit"[^>]*disabled=""/);
  });

  it("marks the question as required", () => {
    render(<ContactForm kind="sales" />);
    expect((screen.getByLabelText("How can we help you?", { exact: false }) as HTMLTextAreaElement).required).toBe(true);
  });
});

describe("wideFields", () => {
  // Two fields to a row; a field left alone in its row takes the whole of
  // it, as does a text of several lines.
  it("leaves no field alone in half a row", () => {
    expect([...wideFields(sales.fields)].sort()).toEqual(["email", "message"]);
    expect([...wideFields(enterprise.fields)].sort()).toEqual(["message"]);
  });
});
