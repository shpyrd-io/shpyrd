import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { accepts, FileDrop, fileSize } from "./file-drop";

const png = (name = "logo.png", size = 100) => new File([new Uint8Array(size)], name, { type: "image/png" });

describe("FileDrop", () => {
  it("takes a file chosen through the input, and one dropped on it", () => {
    const onChange = vi.fn();
    const { container } = render(<FileDrop onChange={onChange} />);
    const input = container.querySelector<HTMLInputElement>("input[type=file]")!;
    fireEvent.change(input, { target: { files: [png("a.png")] } });
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ name: "a.png" }));
    const zone = screen.getByRole("button", { name: "Choose a file" });
    fireEvent.dragOver(zone);
    expect(zone.getAttribute("data-dragging")).toBe("true");
    fireEvent.drop(zone, { dataTransfer: { files: [png("b.png")] } });
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ name: "b.png" }));
    expect(zone.hasAttribute("data-dragging")).toBe(false);
  });

  it("refuses what is too big or of another kind, and says why", () => {
    const onChange = vi.fn();
    const onReject = vi.fn();
    render(<FileDrop accept="image/png" maxSize={1024} onChange={onChange} onReject={onReject} />);
    const zone = screen.getByRole("button", { name: "Choose a file" });
    fireEvent.drop(zone, { dataTransfer: { files: [png("big.png", 4096)] } });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toContain("4 KB");
    fireEvent.drop(zone, { dataTransfer: { files: [new File(["x"], "notes.txt", { type: "text/plain" })] } });
    expect(onReject).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("alert").textContent).toContain("notes.txt");
  });

  it("shows what is there, and offers to change or remove it", () => {
    const onRemove = vi.fn();
    render(<FileDrop preview="data:image/png;base64,iVBORw0KGgo=" onChange={() => {}} onRemove={onRemove} />);
    expect(screen.getByRole("button", { name: "Change the file" }).getAttribute("data-has-file")).toBe("true");
    expect(screen.getByRole("button", { name: "Change" })).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(onRemove).toHaveBeenCalled();
  });

  it("takes nothing while disabled", () => {
    const onChange = vi.fn();
    render(<FileDrop disabled onChange={onChange} />);
    const zone = screen.getByRole("button", { name: "Choose a file" });
    fireEvent.drop(zone, { dataTransfer: { files: [png()] } });
    expect(onChange).not.toHaveBeenCalled();
    expect(zone.getAttribute("aria-disabled")).toBe("true");
  });

  it("knows the kinds an accept names, and writes a size", () => {
    expect(accepts({ name: "a.svg", type: "image/svg+xml" }, "image/png,.svg")).toBe(true);
    expect(accepts({ name: "a.gif", type: "image/gif" }, "image/*")).toBe(true);
    expect(accepts({ name: "a.txt", type: "text/plain" }, "image/*")).toBe(false);
    expect(accepts({ name: "a.txt", type: "text/plain" })).toBe(true);
    expect(fileSize(512)).toBe("512 B");
    expect(fileSize(256 * 1024)).toBe("256 KB");
    expect(fileSize(1.5 * 1024 * 1024)).toBe("1.5 MB");
  });
});
