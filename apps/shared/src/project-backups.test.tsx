// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectBackups } from "./project-backups";
import type { ProjectArchiveActions, ProjectArchiveStatus } from "./api/project-archives";

afterEach(cleanup);

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function setup(status: ProjectArchiveStatus = { phase: "idle" }) {
  const done = deferred();
  const actions: ProjectArchiveActions = {
    status: vi.fn(async () => status), backup: vi.fn(() => done.promise),
    restore: vi.fn(() => done.promise), recover: vi.fn(() => done.promise),
  };
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const element = <QueryClientProvider client={queries}><ProjectBackups name="Example" queryKey="example" actions={actions} /></QueryClientProvider>;
  const view = render(element);
  return { actions, done, element, view, queries };
}
const button = (name: string | RegExp) => screen.getByRole("button", { name }) as HTMLButtonElement;

async function ready() {
  await waitFor(() => expect(button("Download project backup").disabled).toBe(false));
}

describe("project backup and restore feedback", () => {
  it("shows immediate progress before polling changes, and locks every control", async () => {
    const { actions, done } = setup();
    await ready();
    fireEvent.click(button("Download project backup"));
    fireEvent.click(button("Pause and back up"));
    await waitFor(() => expect(button("Preparing backup…").disabled).toBe(true));
    expect(button("Preparing backup…").getAttribute("aria-busy")).toBe("true");
    expect(button("Preparing backup…").querySelector("svg.animate-spin")).not.toBeNull();
    expect(button("Restore project").disabled).toBe(true);
    expect((screen.getByLabelText("Restore from a project archive") as HTMLInputElement).disabled).toBe(true);
    fireEvent.click(button("Preparing backup…"));
    expect(actions.backup).toHaveBeenCalledTimes(1);
    await act(async () => done.resolve());
    await ready();
    expect(screen.getByRole("status").textContent).toContain("Check your browser's downloads");
  });

  it("locks backup and file selection throughout upload and restore", async () => {
    const { actions, done } = setup();
    await ready();
    const file = new File(["archive"], "project.tgz", { type: "application/gzip" });
    fireEvent.change(screen.getByLabelText("Restore from a project archive"), { target: { files: [file] } });
    fireEvent.click(button("Restore project"));
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Example" } });
    fireEvent.click(button("Pause and restore"));
    await waitFor(() => expect(button("Restoring project…").disabled).toBe(true));
    expect(button("Restoring project…").getAttribute("aria-busy")).toBe("true");
    expect(button("Download project backup").disabled).toBe(true);
    expect(screen.getByRole("status").textContent).toContain("Uploading and validating archive");
    expect(actions.restore).toHaveBeenCalledWith(file);
    await act(async () => done.resolve());
    await ready();
    expect(screen.getByRole("status").textContent).toBe("The project has been restored.");
  });

  it("never claims a download occurred in the mock preview", async () => {
    const { done } = setup({ phase: "idle", preview: true });
    await ready();
    expect(screen.getByText("Interactive preview")).toBeTruthy();
    fireEvent.click(button("Download project backup"));
    fireEvent.click(button("Simulate backup"));
    await waitFor(() => expect(button("Preparing backup…").disabled).toBe(true));
    await act(async () => done.resolve());
    await ready();
    expect(screen.getByText("Preview finished. No backup file was generated or downloaded.").getAttribute("role")).toBe("status");
    expect(screen.queryByText(/Your download is starting/)).toBeNull();
  });

  it("keeps controls locked when returning to a project with a pending request", async () => {
    const { done, view, element } = setup();
    await ready();
    fireEvent.click(button("Download project backup"));
    fireEvent.click(button("Pause and back up"));
    await waitFor(() => expect(button("Preparing backup…").disabled).toBe(true));
    view.unmount();
    render(element);
    expect(button("Download project backup").disabled).toBe(true);
    expect(screen.getByRole("status").textContent).toContain("Preparing operation");
    await act(async () => done.resolve());
    await ready();
  });

  it("shows errors and releases the controls when a request fails without maintenance", async () => {
    const { done } = setup();
    await ready();
    fireEvent.click(button("Download project backup"));
    fireEvent.click(button("Pause and back up"));
    await waitFor(() => expect(button("Preparing backup…").disabled).toBe(true));
    await act(async () => done.reject(new Error("No space for the archive")));
    await ready();
    expect(screen.getByText("No space for the archive")).toBeTruthy();
    expect(screen.queryByText(/Backup prepared/)).toBeNull();
  });
});
