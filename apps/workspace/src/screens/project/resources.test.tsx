import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Project, PublicConfig, ResourceInfo, SizeCatalog, VolumeInfo, WorkspaceInfo } from "@/api/types";
import type { Perms } from "@/lib/perms";
import mockConfig from "../../../mock/config.json";
import projects from "../../../mock/projects.json";
import things from "../../../mock/things.json";
import { allowanceOf, BucketDialog, ConvertDialog, detailsOf, gbWords, humanCap, kindOf, ResourceDialog, Resources, SleepDialog, sleepOf, sleepWords, VolumeDialog } from "./resources";
import workspace from "../../../mock/workspace.json";

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

// The plan's words before the request (storage plan step 4): what may
// still be added, what the plan does not include, the count against it.
describe("the ceilings in the dialogs", () => {
  const paid = workspace as unknown as WorkspaceInfo;

  it("works out the allowance from the workspace's ceiling and the project's own cap", () => {
    expect(allowanceOf("500Gi", "120Gi")).toBe(380);
    expect(allowanceOf("500Gi", "120Gi", { storage: "30Gi", ceiling: "20Gi", own: true })).toBe(0);
    expect(allowanceOf("500Gi", "120Gi", { storage: "5Gi", ceiling: "20Gi", own: true })).toBe(15);
    expect(allowanceOf(undefined, undefined)).toBeUndefined();
    expect(gbWords(1.5)).toBe("1.5 GB");
    expect(gbWords(0.5)).toBe("512 MB");
  });

  it("the volume dialog says what may still be added under the ceiling that applies", () => {
    state.answers = { config, workspace: paid, usage: { storage: "5Gi", ceiling: "20Gi", own: true } };
    render(<VolumeDialog slug="hello-world" open onDone={nothing} />);
    expect(screen.getByTestId("allowance").textContent).toBe("You can add up to 15 GB under this project's cap of 20 GB.");
    cleanup();
    state.answers = { config, workspace: paid, usage: { storage: "30Gi", ceiling: "500Gi" } };
    render(<VolumeDialog slug="hello-world" open onDone={nothing} />);
    expect(screen.getByTestId("allowance").textContent).toBe("You can add up to 380 GB under the workspace's 500 GB.");
  });

  it("the volume dialog refuses a disk the free workspace does not include", () => {
    state.answers = { config, workspace: { ...paid, limits: { ...paid.limits, allowDisks: false } }, usage: { storage: "0" } };
    render(<VolumeDialog slug="hello-world" open onDone={nothing} />);
    expect(screen.getByText("Disks are not included in the free workspace")).toBeTruthy();
    expect(screen.getByText("Use a bucket for files, or upgrade.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Make it" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("the new database dialog says the count against the plan, and the kind the plan includes", async () => {
    state.answers = { config: onePlan("shared"), sizes, workspace: { ...paid, limits: { postgres: { types: ["shared"], defaultType: "shared", sharedCap: "1Gi", databases: 1 } }, usage: { ...paid.usage!, databases: 1 } } };
    render(<ResourceDialog slug="hello-world" kind="Postgres" onDone={nothing} onClose={nothing} />);
    await waitFor(() => expect(screen.getByText("This would bring the workspace to 2 of 1 databases.")).toBeTruthy());
    expect(screen.getByText("Your plan includes a shared database. A dedicated database comes with a paid plan.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Make it" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("a volume row says where its growth stands", () => {
    const r: ResourceInfo = { kind: "Volume", name: "uploads", phase: "Bound", details: { size: "20Gi", mode: "single" }, attachedTo: [], data: true, createdAt: "2026-10-01T00:00:00Z" };
    const v = { ...volumes[0], resize: { phase: "pending" as const, to: "20Gi", message: "the provider is growing the disk" } };
    expect(detailsOf(r, v)).toBe("20Gi · single · growth to 20Gi pending: the provider is growing the disk");
    expect(detailsOf(r, { ...v, resize: { phase: "completed", to: "20Gi" } })).toBe("20Gi · single");
  });
});

describe("a bucket beside the application", () => {
  it("says what a bucket is in one line and reads as a bucket", () => {
    expect(detailsOf(named("uploads"))).toBe("up to 10 GB · region us-east-1");
    expect(detailsOf({ ...named("uploads"), details: { capBytes: "0", public: "true", retentionDays: "30", rotating: "2026-10-16T10:00:00Z" } })).toBe("no cap · public read · kept 30 days · key being rotated");
  });

  it("draws its use against its cap, its public door, and offers a new key, attach and remove", () => {
    state.answers = { config, resources, volumes, sizes, workspace };
    const html = renderToStaticMarkup(<Resources project={project} perms={perms} />);
    const uploads = rowOf(html, "uploads");
    expect(uploads).toContain(">Bucket</td>");
    expect(uploads).toContain("3.4 GiB of 10 GiB · 34%");
    expect(uploads).toContain("New key");
    expect(uploads).toContain("Attach");
    expect(uploads).toContain("Remove uploads");
  });

  it("offers a bucket from the Add menu when the cluster has object storage", async () => {
    // A Radix menu opens on the pointer going down on its trigger.
    const open = () => fireEvent.pointerDown(screen.getByRole("button", { name: "Add" }), { button: 0, ctrlKey: false, pointerType: "mouse" });
    state.answers = { config, resources: [], volumes: [], sizes, workspace };
    render(<Resources project={project} perms={perms} />);
    open();
    const item = await screen.findByRole("menuitem", { name: /S3 Compatible Storage/ });
    expect(item.getAttribute("aria-disabled")).not.toBe("true");
    cleanup();
    state.answers = { config: { ...config, extensions: config.extensions.filter((x) => x !== "object-storage") }, resources: [], volumes: [], sizes, workspace };
    render(<Resources project={project} perms={perms} />);
    open();
    expect((await screen.findByRole("menuitem", { name: /S3 Compatible Storage/ })).textContent).toContain("needs the object-storage extension");
  });

  it("says the plan's cap per bucket, and refuses more in the plan's words before the request", async () => {
    state.answers = { workspace };
    render(<BucketDialog slug="hello-world" onDone={nothing} onClose={nothing} />);
    expect(screen.getByText("The plan's cap per bucket, 10 GB, unless you ask for less.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Holds up to"), { target: { value: "50Gi" } });
    await waitFor(() => expect(screen.getByText("A bucket holds up to 10 GB.")).toBeTruthy());
    expect((screen.getByRole("button", { name: "Make it" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("says when the plan's buckets are used up", () => {
    const w = workspace as unknown as WorkspaceInfo;
    state.answers = { workspace: { ...w, limits: { ...w.limits, objectStorage: { buckets: 1, bucketCap: "1Gi" } }, usage: { ...w.usage, buckets: 1 } } };
    render(<BucketDialog slug="hello-world" onDone={nothing} onClose={nothing} />);
    expect(screen.getByText("This would bring the workspace to 2 of 1 buckets.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Make it" }) as HTMLButtonElement).disabled).toBe(true);
  });
});

// Sleep (#120, #132): the three states of #135 read apart, on the web row
// and on a dedicated database, and the dialog that sets them.
describe("what a sleep policy says and how it is set", () => {
  it("reads the three states apart, with the wake mode and the state now", () => {
    expect(sleepWords({ state: "awake", source: "off" })).toBe("never sleeps, whatever the workspace's default");
    expect(sleepWords({ state: "awake", after: "30m", resuming: "wait", source: "project" })).toBe("sleeps after 30m (wait mode), its own policy");
    expect(sleepWords({ state: "sleeping", after: "15m", resuming: "page", source: "workspace" })).toBe("sleeps after 15m (page mode), the workspace's default; asleep now");
    expect(sleepWords({ state: "awake", source: "workspace" })).toBe("does not sleep: the workspace's default sets none");
    expect(sleepWords({ state: "suspended", after: "10m", source: "workspace" })).toBe("sleeps after 10m, the workspace's default; suspended now");
  });
  it("reads a database's policy out of its details, and says it in the details line only when something applies", () => {
    expect(sleepOf({ sleepState: "sleeping", sleepAfter: "10m0s", sleepSource: "workspace" })).toEqual({ state: "sleeping", after: "10m0s", source: "workspace", message: undefined });
    expect(detailsOf(named("db"))).not.toContain("sleep");
    expect(detailsOf({ ...named("db"), details: { ...named("db").details, sleepAfter: "10m0s", sleepState: "awake" } })).toContain("sleeps after 10m0s, the workspace's default");
    expect(detailsOf({ ...named("db"), details: { ...named("db").details, sleepSource: "off" } })).toContain("never sleeps");
  });
  it("shows the web process's policy on its row, and a Sleep control", () => {
    state.answers = { resources, volumes, config, sizes, workspace };
    const asleep = projects.find((p) => p.processes?.web?.sleep?.state === "sleeping") as unknown as Project;
    const html = renderToStaticMarkup(<Resources project={asleep} perms={perms} />);
    expect(html).toContain("sleeps after 15m (page mode), the workspace&#x27;s default; asleep now");
    expect(html).toContain(">Sleep<");
  });
  it("offers the workspace's default, a quiet period of its own with the wake mode for a web process, and off", () => {
    const onSave = vi.fn();
    render(<SleepDialog what="shop" current={{ state: "awake", after: "15m", resuming: "page", source: "workspace" }} modes onSave={onSave} />);
    fireEvent.click(screen.getByRole("button", { name: "Sleep" }));
    expect(screen.getByText("When shop sleeps")).toBeTruthy();
    expect(screen.getByText(/Now it sleeps after 15m \(page mode\), the workspace's default\./)).toBeTruthy();
    // The default is kept as it is: saving hands the project back to the workspace.
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave).toHaveBeenCalledWith({ after: "default" });
  });
  it("a quiet period of its own shows the period and, for a web process, the wake mode", () => {
    const onSave = vi.fn();
    render(<SleepDialog what="shop" current={{ state: "awake", after: "30m", resuming: "wait", source: "project" }} modes onSave={onSave} />);
    fireEvent.click(screen.getByRole("button", { name: "Sleep" }));
    expect(screen.getByText("Quiet period")).toBeTruthy();
    expect(screen.getByText("Waking")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave).toHaveBeenCalledWith({ after: "30m", resuming: "wait" });
  });
  it("a database has no wake mode, and off is a policy of its own", () => {
    const onSave = vi.fn();
    render(<SleepDialog what="db" current={{ state: "awake", source: "off" }} onSave={onSave} />);
    fireEvent.click(screen.getByRole("button", { name: "Sleep" }));
    expect(screen.getByText(/Now it never sleeps, whatever the workspace's default\./)).toBeTruthy();
    expect(screen.queryByText("Waking")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave).toHaveBeenCalledWith({ after: "off" });
  });
});
