import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ShellView } from "./shell-view";

// The terminal needs a browser that jsdom is not: here it is only a box.
vi.mock("./terminal", () => ({
  Terminal: (props: { height?: number }) => <div data-slot="terminal" style={{ height: props.height }} />,
}));

const instances = [
  { name: "web-1", ready: true },
  { name: "worker-1", ready: false },
];

describe("ShellView", () => {
  it("offers to connect once an instance is chosen, and says what it waits for", () => {
    const onConnect = vi.fn();
    const { rerender } = render(
      <ShellView instances={instances} onInstanceChange={() => {}} status={{ kind: "picking" }} onConnect={onConnect} onDisconnect={() => {}} />,
    );
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Connect" }).disabled).toBe(true);
    expect(screen.getByText(/One shell per project/)).not.toBeNull();
    rerender(
      <ShellView instances={instances} instance="web-1" onInstanceChange={() => {}} status={{ kind: "picking" }} onConnect={onConnect} onDisconnect={() => {}} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(onConnect).toHaveBeenCalledOnce();
  });

  it("offers to disconnect while it is open, and says where it is", () => {
    const onDisconnect = vi.fn();
    render(
      <ShellView instances={instances} instance="web-1" onInstanceChange={() => {}} status={{ kind: "open", instance: "web-1", shell: "bash" }} onConnect={() => {}} onDisconnect={onDisconnect} />,
    );
    expect(screen.getByText("web-1 · bash")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(onDisconnect).toHaveBeenCalledOnce();
  });

  it("offers to reconnect after it closed, with the reason", () => {
    render(
      <ShellView instances={instances} instance="web-1" onInstanceChange={() => {}} status={{ kind: "closed", instance: "web-1", message: "Session ended" }} onConnect={() => {}} onDisconnect={() => {}} />,
    );
    expect(screen.getByRole("button", { name: "Reconnect" })).not.toBeNull();
    expect(screen.getByText("Session ended")).not.toBeNull();
  });

  it("says when there is no instance to open", () => {
    render(<ShellView instances={[]} onInstanceChange={() => {}} status={{ kind: "picking" }} onConnect={() => {}} onDisconnect={() => {}} />);
    expect(screen.getByText("No running instance")).not.toBeNull();
  });
});
