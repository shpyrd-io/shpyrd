import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Field } from "./field";
import { Input } from "./input";

describe("Field", () => {
  it("ties the label to the field, with no id written", () => {
    render(
      <Field label="Project name">
        <Input />
      </Field>,
    );
    const field = screen.getByLabelText("Project name");
    expect(field.tagName).toBe("INPUT");
    expect(field.id).not.toBe("");
  });

  it("keeps the id the field already has", () => {
    render(
      <Field label="Address">
        <Input id="address" />
      </Field>,
    );
    expect(screen.getByLabelText("Address").id).toBe("address");
  });

  it("tells the field of its hint and of its error, and marks it as not valid", () => {
    render(
      <Field label="Address" hint="Lowercase letters." error="No spaces.">
        <Input />
      </Field>,
    );
    const field = screen.getByLabelText("Address");
    const told = (field.getAttribute("aria-describedby") ?? "").split(" ");
    expect(told.map((id) => document.getElementById(id)?.textContent)).toEqual([
      "Lowercase letters.",
      "No spaces.",
    ]);
    expect(field.getAttribute("aria-invalid")).toBe("true");
  });

  it("says nothing of an error there is not", () => {
    render(
      <Field label="Address">
        <Input />
      </Field>,
    );
    const field = screen.getByLabelText("Address");
    expect(field.hasAttribute("aria-invalid")).toBe(false);
    expect(field.hasAttribute("aria-describedby")).toBe(false);
  });

  it("marks a field that is required, in the field and beside the label", () => {
    render(
      <Field label="Owner" required>
        <Input />
      </Field>,
    );
    expect(screen.getByRole<HTMLInputElement>("textbox").required).toBe(true);
    expect(screen.getByText("*").getAttribute("aria-hidden")).toBe("true");
  });

  it("reaches the field inside a box, when the input has something beside its text", () => {
    render(
      <Field label="Address" error="No spaces.">
        <Input suffix=".shpyrd.app" />
      </Field>,
    );
    expect(screen.getByLabelText("Address").getAttribute("aria-invalid")).toBe("true");
  });
});
