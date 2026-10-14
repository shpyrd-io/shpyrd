import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DomainsAnswer } from "@/api/types";
import { DomainsCard } from "./domains";

let queries: QueryClient;
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});
afterEach(() => { cleanup(); queries?.clear(); });
function show(canEdit: boolean, redirectTo = "") {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const answer: DomainsAnswer = { target: "shop.shpyrd.app", redirectTo, domains: [
    { host: "ready.example.com", dns: "ok", certificate: "ready", redirectEligible: true },
    { host: "waiting.example.com", dns: "missing", certificate: "issuing" },
  ] };
  const redirect = vi.fn().mockResolvedValue(answer);
  render(<QueryClientProvider client={queries}><DomainsCard title="Domains" description="Project domains" queryKey={["domains", "shop"]} list={async () => answer} add={vi.fn()} remove={vi.fn()} redirect={redirect} canEdit={canEdit} /></QueryClientProvider>);
  return redirect;
}
describe("project domain redirects", () => {
  it("offers only eligible domains and keeps the workspace default available", async () => {
    show(true);
    const select = await screen.findByRole("combobox", { name: "Redirect the platform address" });
    fireEvent.click(select);
    expect(await screen.findByRole("option", { name: "ready.example.com" })).toBeTruthy();
    expect(screen.queryByRole("option", { name: "waiting.example.com" })).toBeNull();
    expect(screen.getByRole("option", { name: "Use workspace default" })).toBeTruthy();
  });
  it("shows the selected target to viewers without allowing changes", async () => {
    show(false, "ready.example.com");
    const select = await screen.findByRole("combobox", { name: "Redirect the platform address" });
    expect(select.hasAttribute("disabled")).toBe(true);
    expect(select.textContent).toContain("ready.example.com");
    expect(screen.getByText("Redirects activate only while the destination DNS and certificate are ready.")).toBeTruthy();
  });
});
