// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { ContactForm } from "./contact-form";
import { trap } from "@/lib/contact";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function fill() {
  for (const [label, value] of [["First name", "Ana"], ["Last name", "Souza"], ["Work email", "ana@acme.com"], ["Company", "Acme"], ["How can we help you?", "Six apps to move."]]) {
    fireEvent.change(screen.getByLabelText(label, { exact: false }), { target: { value } });
  }
}

describe("ContactForm", () => {
  it("shows the sales form's fields", () => {
    render(<ContactForm kind="sales" />);
    for (const label of ["First name", "Last name", "Work email", "Company", "How can we help you?"]) {
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
    expect((screen.getByLabelText("Company", { exact: false }) as HTMLInputElement).value).toBe("Acme");
    expect(screen.getAllByRole("link", { name: /Discord/ })[0].getAttribute("href")).toBe("/discord");
  });

  it("shows the enterprise form's choices as checkboxes", () => {
    render(<ContactForm kind="enterprise" />);
    expect(screen.getByRole("checkbox", { name: "Single sign-on" })).toBeTruthy();
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

  it("ties the checkboxes to the error the server gives them", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ errors: { needs: "Choose from the options." } }), { status: 400 })));
    const { container } = render(<ContactForm kind="enterprise" />);
    for (const [label, value] of [["First name", "Ana"], ["Last name", "Souza"], ["Work email", "ana@acme.com"], ["Company", "Acme"], ["Job title", "CTO"]]) {
      // The exact label (with its required mark): "Company" is also in "Company size".
      fireEvent.change(screen.getByLabelText(new RegExp(`^${label}\\*?$`)), { target: { value } });
    }
    for (const [name, value] of [["size", "50-249"], ["runsOn", "cloud"], ["timeline", "quarter"]]) {
      fireEvent.change(container.querySelector(`select[name=${name}]`)!, { target: { value } });
    }
    fireEvent.click(screen.getByRole("button", { name: "Contact enterprise sales" }));
    await waitFor(() => expect(screen.getByText("Choose from the options.")).toBeTruthy());
    const group = screen.getByRole("group", { name: "What do you need?" });
    expect(document.getElementById(group.getAttribute("aria-describedby") ?? "")?.textContent).toBe("Choose from the options.");
  });

  // As the server sends it, before the page's script runs: a click then must
  // not send the answers as a GET, into the address bar and the logs.
  it("cannot be sent before the page is ready, and never by GET", () => {
    const html = renderToString(<ContactForm kind="sales" />);
    expect(html).toMatch(/<form[^>]*method="post"/);
    expect(html).toMatch(/<button[^>]*type="submit"[^>]*disabled=""/);
  });
});
