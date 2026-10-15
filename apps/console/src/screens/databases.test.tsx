import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PostgresServer, PostgresServers, PostgresTenant, PostgresTenantPage } from "@/api/types";
import serversFile from "../../mock/postgres-servers.json";
import tenantsFile from "../../mock/postgres-tenants.json";
import { arrange, Databases } from "./databases";

// The screen's queries answer from here, each by what it asks for. The
// keys asked are kept, so a test sees what a search or a page asked.
const state = vi.hoisted(() => ({
  servers: undefined as PostgresServers | undefined,
  found: [] as PostgresTenant[],
  page: undefined as PostgresTenantPage | undefined,
  asked: [] as unknown[][],
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey, enabled = true }: { queryKey: unknown[]; enabled?: boolean }) => {
    state.asked.push(queryKey);
    switch (queryKey[0]) {
      case "postgres-servers":
        return { data: state.servers };
      case "postgres-find":
        return { data: enabled ? state.found : undefined };
      case "postgres-tenants":
        return { data: state.page };
      default:
        return {};
    }
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  state.asked = [];
});

// The three servers and the named databases of the Mock's files.
const fleet = serversFile as unknown as PostgresServers;
const tenant = (tenantsFile as { tenants: PostgresTenant[] }).tenants[0];
const servers = (attention: Partial<PostgresServers["attention"]> = {}): PostgresServers => ({
  ...fleet,
  attention: { paused: 0, over: 0, near: 0, degraded: 0, dumpsFailed: 0, ...attention },
});
const names = (list: PostgresServer[]) => list.map((s) => s.name);

// The page as a document, read by where things are: a Stat by its label,
// a server's row by its name.
const page = () => new DOMParser().parseFromString(renderToStaticMarkup(<Databases />), "text/html");
function stat(doc: Document, label: string) {
  const el = [...doc.querySelectorAll("[data-slot=stat]")].find((s) => s.firstElementChild?.textContent === label);
  const [value, unit] = [...(el?.querySelectorAll("span") ?? [])].map((s) => s.textContent);
  return { value, unit };
}
function row(doc: Document, name: string) {
  const tr = [...doc.querySelectorAll("tr")].find((r) => r.querySelector("code")?.textContent === name);
  return { cells: [...(tr?.querySelectorAll("td") ?? [])].map((td) => td.textContent ?? ""), failed: tr?.querySelector(".text-destructive")?.textContent };
}

describe("the databases of the platform", () => {
  it("counts what needs attention across the fleet", () => {
    state.servers = servers({ paused: 2, over: 3, near: 5, degraded: 1, dumpsFailed: 1 });
    let doc = page();
    expect(stat(doc, "Needs attention")).toEqual({ value: "7", unit: "2 paused · 3 over cap · 1 server not healthy · dumps failed on 1" });
    expect(stat(doc, "Shared databases")).toEqual({ value: "388", unit: "on 3 servers" });
    expect(stat(doc, "Room for new ones")).toEqual({ value: "212", unit: "2 servers open" });
    expect(stat(doc, "Dedicated clusters")).toEqual({ value: "13", unit: "12 ready · 11 backed up · 605.4 GiB" });

    // Near its cap is the owner's to watch; it is not counted. Two
    // servers down are written in the plural.
    state.servers = servers({ near: 5 });
    expect(stat(page(), "Needs attention")).toEqual({ value: "0", unit: "nothing" });
    state.servers = servers({ degraded: 2 });
    expect(stat(page(), "Needs attention")).toEqual({ value: "2", unit: "2 servers not healthy" });
  });

  it("orders the servers fullest first and keeps only the open ones when asked", () => {
    const all = fleet.servers;
    expect(names(arrange(all, "all", "fill", ""))).toEqual(["pg-shared-1", "pg-shared-2", "pg-shared-3"]);
    expect(names(arrange(all, "open", "fill", ""))).toEqual(["pg-shared-2", "pg-shared-3"]);
    expect(names(arrange(all, "full", "name", ""))).toEqual(["pg-shared-1"]);
    expect(names(arrange(all, "unhealthy", "name", ""))).toEqual(["pg-shared-3"]);
    // Dumps that failed, and a server that has not dumped yet.
    expect(names(arrange(all, "dumps", "name", ""))).toEqual(["pg-shared-2", "pg-shared-3"]);
    expect(names(arrange(all, "all", "disk", ""))).toEqual(["pg-shared-2", "pg-shared-1", "pg-shared-3"]);
    expect(names(arrange(all, "all", "connections", ""))).toEqual(["pg-shared-1", "pg-shared-2", "pg-shared-3"]);
    expect(names(arrange(all, "all", "name", " shared-3 "))).toEqual(["pg-shared-3"]);
    // By name is by the number: ten comes after three.
    const tenth = { ...all[0], name: "pg-shared-10" };
    expect(names(arrange([tenth, ...all].reverse(), "all", "name", ""))).toEqual(["pg-shared-1", "pg-shared-2", "pg-shared-3", "pg-shared-10"]);

    state.servers = servers();
    const doc = page();
    expect([...doc.querySelectorAll("tbody code")].map((c) => c.textContent)).toEqual(["pg-shared-1", "pg-shared-2", "pg-shared-3"]);
    expect(doc.body.textContent).toContain("3 of 3");
  });

  it("writes a server's state with its backup and its failed dumps", () => {
    vi.setSystemTime(new Date("2026-10-15T13:05:00Z"));
    const failing: PostgresServer = { ...fleet.servers[1], name: "pg-shared-4", phase: "Failing over", archiving: "failing", lastDumps: { at: "2026-10-15T03:20:00Z", dumped: 184, failed: 3, took: "70s" } };
    state.servers = { ...servers(), servers: [...fleet.servers, failing] };
    const doc = page();

    const second = row(doc, "pg-shared-2");
    expect(second.cells[1]).toContain("Healthy");
    expect(second.cells[1]).toContain("2/2 · backup 11h ago · dumps 9h ago, 1 failed");
    expect(second.failed).toBe("dumps 9h ago, 1 failed");
    expect(second.cells[2]).toContain("187/200");
    expect(second.cells[3]).toContain("41/50 GiB");
    expect(second.cells[3]).toContain("growing");
    expect(second.cells[4]).toContain("no pooler");

    const first = row(doc, "pg-shared-1");
    expect(first.cells[1]).toContain("2/2 · backup 11h ago · dumps 9h ago");
    expect(first.failed).toBeUndefined();
    expect(first.cells[2]).toContain("full · 3 kept · 1 converting");
    expect(first.cells[4]).toContain("pooler on");

    // One instance of two still coming, nothing archived or dumped yet.
    const third = row(doc, "pg-shared-3");
    expect(third.cells[1]).toContain("Coming up");
    expect(third.cells[1]).toContain("1/2 · archiving starting · no dumps yet");

    const fourth = row(doc, "pg-shared-4");
    expect(fourth.cells[1]).toContain("Degraded");
    expect(fourth.cells[1]).toContain("2/2 · archiving failing · dumps 9h ago, 3 failed");
  });

  it("finds a database across every server by its hashed name", () => {
    state.servers = servers();
    state.found = [tenant];
    render(<Databases />);
    expect(screen.queryByText(tenant.database)).toBeNull();

    fireEvent.change(screen.getByRole("textbox", { name: "Find a database" }), { target: { value: "db_3488978c" } });
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    expect(state.asked).toContainEqual(["postgres-find", "db_3488978c"]);
    const found = screen.getByText("db_3488978c").closest("tr")!;
    expect(found.textContent).toContain("acme / shop");
    expect(found.textContent).toContain("pg-shared-1");
    expect(found.textContent).toContain("298 MiB of 1.0 GiB");
    expect(found.textContent).toContain("Ready");

    state.found = [];
    fireEvent.change(screen.getByRole("textbox", { name: "Find a database" }), { target: { value: "nobody" } });
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    expect(screen.getByText("No database matches “nobody”.")).toBeTruthy();
  });

  it("pages a server's databases fifty at a time", () => {
    state.servers = servers();
    state.page = { items: Array.from({ length: 50 }, (_, i) => ({ ...tenant, database: `db_${i}` })), total: 200, page: 1, size: 50 };
    render(<Databases />);
    fireEvent.click(screen.getByRole("button", { name: "The databases on pg-shared-1" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("Databases on pg-shared-1");
    expect(dialog.textContent).toContain("200 of 200; 3 kept behind a drop date, 1 converting.");
    expect(within(dialog).getAllByRole("row")).toHaveLength(51);
    expect(dialog.textContent).toContain("200 databases · page 1 of 4");
    expect(state.asked).toContainEqual(["postgres-tenants", "pg-shared-1", { page: 1, size: 50, state: "", q: "" }]);
    expect((within(dialog).getByRole("button", { name: "Previous page" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(within(dialog).getByRole("button", { name: "Next page" }));
    expect(dialog.textContent).toContain("200 databases · page 2 of 4");
    expect(state.asked).toContainEqual(["postgres-tenants", "pg-shared-1", { page: 2, size: 50, state: "", q: "" }]);

    // Narrowing the list starts over from the first page.
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Narrow the list" }), { target: { value: "acme" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Narrow the list" }));
    expect(state.asked).toContainEqual(["postgres-tenants", "pg-shared-1", { page: 1, size: 50, state: "", q: "acme" }]);
    expect(dialog.textContent).toContain("200 databases match · page 1 of 4");
  });
});
