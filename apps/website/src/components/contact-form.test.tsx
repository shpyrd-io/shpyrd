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
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/contact");
    const body = JSON.parse(String(init.body));
    expect(body).toMatchObject({ kind: "sales", website: "", answers: { firstName: "Ana", interest: "cloud" } });
    expect(body.elapsedMs).toBeGreaterThanOrEqual(0);
    expect(body).not.toHaveProperty("startedAt");
  });

  it("keeps what was typed and offers Discord when it cannot send", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "x" }), { status: 502 })));
    const { container } = render(<ContactForm kind="sales" />);
    fill(container);
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
});
