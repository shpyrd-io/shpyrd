import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ConfirmDialog } from "./confirm-dialog";

const destroy = (onConfirm = vi.fn()) => {
  render(
    <ConfirmDialog
      trigger={<button>Destroy</button>}
      variant="destructive"
      title="Destroy the project?"
      description="This cannot be undone."
      confirmation="hello-world"
      action="Destroy the project"
      onConfirm={onConfirm}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Destroy" }));
  return onConfirm;
};

describe("ConfirmDialog", () => {
  it("asks before it acts, and acts when it is confirmed", () => {
    const restart = vi.fn();
    render(
      <ConfirmDialog
        trigger={<button>Restart</button>}
        title="Restart the instances?"
        action="Yes, restart"
        onConfirm={restart}
      />,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Restart" }));
    expect(screen.getByRole("dialog", { name: "Restart the instances?" })).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Yes, restart" }));
    expect(restart).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does nothing when it is cancelled", () => {
    const onConfirm = destroy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onConfirm).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the action disabled until the text is typed, the whole of it", () => {
    destroy();
    const action = screen.getByRole<HTMLButtonElement>("button", { name: "Destroy the project" });
    const field = screen.getByRole("textbox");
    expect(action.disabled).toBe(true);

    fireEvent.change(field, { target: { value: "hello-worl" } });
    expect(action.disabled).toBe(true);
    fireEvent.change(field, { target: { value: "Hello-World" } });
    expect(action.disabled).toBe(true);
    fireEvent.change(field, { target: { value: "hello-world" } });
    expect(action.disabled).toBe(false);
  });

  it("cannot be confirmed by sending the form before the text is typed", () => {
    const onConfirm = destroy();
    fireEvent.submit(screen.getByRole("textbox").closest("form") as HTMLFormElement);
    expect(onConfirm).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("acts and closes when the text is typed and the form is sent", () => {
    const onConfirm = destroy();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "hello-world" } });
    fireEvent.submit(screen.getByRole("textbox").closest("form") as HTMLFormElement);
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("forgets what was typed when it is opened again", () => {
    destroy();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "hello-world" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Destroy" }));
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe("");
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Destroy the project" }).disabled,
    ).toBe(true);
  });

  it("says what has to be typed, tied to the field", () => {
    destroy();
    expect(screen.getByLabelText("Type hello-world to confirm")).toBe(screen.getByRole("textbox"));
  });
});
