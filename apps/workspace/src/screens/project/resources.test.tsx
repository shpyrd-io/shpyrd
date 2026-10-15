import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Project, PublicConfig, ResourceInfo, SizeCatalog, VolumeInfo } from "@/api/types";
import type { Perms } from "@/lib/perms";
import mockConfig from "../../../mock/config.json";
import projects from "../../../mock/projects.json";
import things from "../../../mock/things.json";
import { ConvertDialog, detailsOf, humanCap, kindOf, ResourceDialog, Resources } from "./resources";

// The queries answer from the Mock's files, by the first word of their
// key: "resources", "volumes", "config", "sizes".
const state = vi.hoisted(() => ({ answers: {} as Record<string, unknown> }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({ data: state.answers[String(queryKey[0])], isLoading: false, error: undefined }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

// The project hello-world of the Mock: analytics is shared, events is
// shared and over its cap, db is dedicated.
const hello = things.projects["hello-world"];
const resources = hello.resources as unknown as ResourceInfo[];
const volumes = hello.volumes as unknown as VolumeInfo[];
const sizes = things.sizes as unknown as SizeCatalog;
const config = mockConfig as unknown as PublicConfig;
const project = projects[0] as unknown as Project;
const perms = { resource: true } as Perms;
const named = (name: string) => resources.find((r) => r.name === name)!;
// A plan with one kind of database only.
const onePlan = (kind: "shared" | "dedicated"): PublicConfig => ({ ...config, postgres: { types: [kind], defaultType: kind, sharedCap: "1Gi" } });
const nothing = () => {};
// The table row of a resource, out of the card's markup.
const rowOf = (html: string, name: string) => html.split("<tr").find((row) => row.includes(`>${name}</code>`)) ?? "";
// The Kind select comes first in the new database form. A select is not
// named by what it shows, so it is found by its place and read by its text.
const kindSelect = () => screen.getAllByRole("combobox")[0];

beforeEach(() => {
  // The morning after the night's copies: 03:07 was six hours ago.
  vi.setSystemTime(new Date("2026-10-15T09:07:00Z"));
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  state.answers = {};
});

describe("what a database's details say", () => {
  it("says what a shared database is in one line", () => {
    expect(detailsOf(named("analytics"))).toBe("shared · PostgreSQL 17 · via the pooler · 7 nightly copies, last 6h ago");
    expect(detailsOf({ ...named("analytics"), details: { type: "shared", version: "17", pooler: "on", backups: "nightly, kept 14" } })).toBe("shared · PostgreSQL 17 · via the pooler · 14 nightly copies, none yet");
  });
  it("says what a dedicated database is in one line", () => {
    expect(detailsOf(named("db"))).toBe("dedicated · v16 · shared-m · 10Gi · 1 instance · backups daily, kept 14d, last 7h ago");
    // A database from before there were kinds has no type: it is dedicated.
    expect(detailsOf({ ...named("db"), details: { version: "16", size: "shared-m", instances: "2" } })).toBe("dedicated · v16 · shared-m · 2 instances");
  });
  it("reads the kind from the details, dedicated when there is none", () => {
    expect(kindOf(named("analytics"))).toBe("shared");
    expect(kindOf(named("db"))).toBe("dedicated");
    expect(kindOf({ ...named("db"), details: undefined })).toBe("dedicated");
  });
  it("humanCap says 1 GB for 1Gi", () => {
    expect(humanCap("1Gi")).toBe("1 GB");
    expect(humanCap("1.5Gi")).toBe("1.5 GB");
    expect(humanCap("2G")).toBe("2 GB");
    expect(humanCap("512Mi")).toBe("512 MB");
    expect(humanCap("a lot")).toBe("a lot");
  });
});

describe("the table beside the application", () => {
  it("shows a shared database over its cap as paused with the server's words", () => {
    state.answers = { config, resources, volumes, sizes };
    const html = renderToStaticMarkup(<Resources project={project} perms={perms} />);
    const events = rowOf(html, "events");
    expect(events).toContain("Paused</span>");
    expect(events).toContain('data-type="warning"');
    expect(events).toContain("Connections are paused: your database is at 152% of its 1Gi cap. Free space, or raise the limit; connections return below 95%.");
    expect(events).toContain("1.5 GiB of 1 GiB · 152%");
    // Under its cap, a shared database is Ready, with its gauge and
    // without the server's message; it converts, it does not resize.
    const analytics = rowOf(html, "analytics");
    expect(analytics).toContain("Ready</span>");
    expect(analytics).toContain("shared · PostgreSQL 17 · via the pooler · 7 nightly copies, last 6h ago");
    expect(analytics).toContain("312 MiB of 1 GiB · 30%");
    expect(analytics).not.toContain("through its connection pooler");
    expect(analytics).toContain(">Convert</button>");
    expect(analytics).not.toContain(">Resize</button>");
    // A dedicated one has a size to change and no gauge.
    const db = rowOf(html, "db");
    expect(db).toContain("dedicated · v16 · shared-m · 10Gi · 1 instance · backups daily, kept 14d, last 7h ago");
    expect(db).not.toContain(" of 1 GiB");
    expect(db).toContain(">Resize</button>");
    expect(db).toContain(">Convert</button>");
  });
});

describe("the convert dialog", () => {
  it("the convert dialog says what follows, to dedicated and to shared", () => {
    state.answers = { sizes };
    render(<ConvertDialog slug="hello-world" resource={named("analytics")} config={config} onDone={nothing} />);
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
    expect(screen.getByText("Convert analytics to a dedicated database?")).toBeTruthy();
    expect(screen.getByText("analytics becomes a cluster of its own with a 5Gi disk, priced as a dedicated database.")).toBeTruthy();
    expect(screen.getByText("Its user and database names stay; DATABASE_URL changes only its host, and the apps bound to it restart once.")).toBeTruthy();
    expect(screen.getByText("Disk")).toBeTruthy();
    expect(screen.getByText("Instance size")).toBeTruthy();
    cleanup();
    render(<ConvertDialog slug="hello-world" resource={named("db")} config={config} onDone={nothing} />);
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
    expect(screen.getByText("Convert db to a shared database?")).toBeTruthy();
    expect(screen.getByText("db moves onto the platform's shared server with a 1 GB cap; its disk goes.")).toBeTruthy();
    expect(screen.getByText("It gets new user and database names, so the apps bound to it restart once with the new credentials. Only the extensions a shared database can hold may be installed: PostgreSQL's trusted ones and pgvector.")).toBeTruthy();
    expect(screen.queryByText("Disk")).toBeNull();
    expect(screen.queryByText("Instance size")).toBeNull();
  });
  it("is not offered when the plan has only the database's own kind", () => {
    expect(renderToStaticMarkup(<ConvertDialog slug="hello-world" resource={named("db")} config={onePlan("dedicated")} onDone={nothing} />)).toBe("");
    expect(renderToStaticMarkup(<ConvertDialog slug="hello-world" resource={named("analytics")} config={onePlan("shared")} onDone={nothing} />)).toBe("");
  });
});

describe("the new database dialog", () => {
  it("the new database dialog offers the plan's kinds, shared by default, and hides the dedicated fields", async () => {
    state.answers = { sizes, config };
    render(<ResourceDialog slug="hello-world" kind="Postgres" onDone={nothing} onClose={nothing} />);
    expect(screen.getByText("New Postgres database")).toBeTruthy();
    expect(screen.getByText(/holds up to 1 GB, PostgreSQL 17, a nightly copy kept/)).toBeTruthy();
    expect(screen.getByText(/the cap of 1 GB is what the database is metered by/)).toBeTruthy();
    expect(screen.getByText("Copies kept")).toBeTruthy();
    for (const label of ["Instance size", "PostgreSQL", "Storage", "Instances", "Backups"]) expect(screen.queryByText(label)).toBeNull();
    const kind = kindSelect();
    await waitFor(() => expect(kind.textContent).toContain("Shared · on the platform's server, up to 1 GB"));
    fireEvent.click(kind);
    expect(await screen.findByRole("option", { name: "Shared · on the platform's server, up to 1 GB" })).toBeTruthy();
    fireEvent.click(screen.getByRole("option", { name: "Dedicated · a cluster of its own, with a disk" }));
    expect(await screen.findByText("Instance size")).toBeTruthy();
    for (const label of ["PostgreSQL", "Storage", "Instances", "Backups"]) expect(screen.getByText(label)).toBeTruthy();
    expect(screen.queryByText("Copies kept")).toBeNull();
  });
  it("keeps to the plan's one kind of database", async () => {
    state.answers = { sizes, config: onePlan("dedicated") };
    render(<ResourceDialog slug="hello-world" kind="Postgres" onDone={nothing} onClose={nothing} />);
    expect(screen.getByText("The workspace's plan has dedicated databases.")).toBeTruthy();
    const kind = kindSelect();
    await waitFor(() => expect(kind.textContent).toContain("Dedicated · a cluster of its own, with a disk"));
    expect(kind.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText("Instance size")).toBeTruthy();
    expect(screen.queryByText("Copies kept")).toBeNull();
  });
});
