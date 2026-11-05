import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import projects from "../../../mock/projects.json";
import { callsWords, Connections } from "./access";

// The queries answer from the Mock's files, by the first word of their key.
const state = vi.hoisted(() => ({ answers: {} as Record<string, unknown> }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({ data: state.answers[String(queryKey[0])], isLoading: false, error: undefined }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

const project = projects[0] as unknown as Project;
const perms = { members: true } as Perms;

afterEach(() => {
  cleanup();
  state.answers = {};
  toast.success.mockClear();
});

describe("the Connections card", () => {
  it("says the address a project let in calls, short and in full, and who called", () => {
    state.answers = { allow: [{ project: "docs" }], projects };
    render(<Connections project={project} perms={perms} />);
    expect(screen.getByText("http://hello-world.internal")).toBeTruthy();
    expect(screen.getByText("http://hello-world.acme.internal")).toBeTruthy();
    expect(screen.getByText("X-Shpyrd-Service")).toBeTruthy();
  });
  it("copies the short address", () => {
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    state.answers = { allow: [], projects };
    render(<Connections project={project} perms={perms} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy the internal address" }));
    expect(writeText).toHaveBeenCalledWith("http://hello-world.internal");
    expect(toast.success).toHaveBeenCalledWith("Copied", { description: "http://hello-world.internal" });
  });
  it("says each caller's day beside it, when there are metrics", () => {
    state.answers = {
      allow: [{ project: "docs" }, { project: "billing" }, { platform: "mcp" }],
      calls: { available: true, callers: [{ caller: "docs", calls: 1204, lastAt: "2026-10-15T09:05:00Z" }] },
      projects,
    };
    render(<Connections project={project} perms={perms} />);
    expect(screen.getByText("Calls, last 24 hours")).toBeTruthy();
    expect(screen.getByText(/1,204 · last/)).toBeTruthy();
    expect(screen.getByText("none")).toBeTruthy(); // billing is let in and has not called
    expect(screen.getByText("—")).toBeTruthy(); // the platform's calls are not counted here
  });
  it("shows no calls column where there are no metrics", () => {
    state.answers = { allow: [{ project: "docs" }], calls: { available: false, callers: [] }, projects };
    render(<Connections project={project} perms={perms} />);
    expect(screen.queryByText("Calls, last 24 hours")).toBeNull();
  });
  it("callsWords says the count and the last call, or none", () => {
    expect(callsWords(undefined)).toBe("none");
    expect(callsWords({ caller: "docs", calls: 0 })).toBe("none");
    expect(callsWords({ caller: "docs", calls: 7 })).toBe("7");
    expect(callsWords({ caller: "docs", calls: 1204, lastAt: new Date(Date.now() - 120_000).toISOString() })).toMatch(/^1,204 · last /);
  });
  it("shows the address even while nothing is let in", () => {
    state.answers = { allow: [], projects };
    render(<Connections project={project} perms={perms} />);
    expect(screen.getByText("http://hello-world.internal")).toBeTruthy();
    expect(screen.getByText(/Nothing is let in/)).toBeTruthy();
  });
});
