import { expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AdvancedIndex } from "./advanced";

it("lists every page under its group, each with the line that says what it is for", () => {
  const pages = [
    { group: "Run", slug: "logs", title: "Logs", icon: <svg /> },
    { group: "Run", slug: "shell", title: "Shell", icon: <svg /> },
    { group: "Access", slug: "roles", title: "Roles", icon: <svg /> },
  ];
  render(<MemoryRouter><AdvancedIndex base="/projects/shop" description="All of it." pages={pages} lines={{ logs: "What it prints." }} /></MemoryRouter>);
  expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual(["Run", "Access"]);
  expect(screen.getByRole("link", { name: /Logs/ }).getAttribute("href")).toBe("/projects/shop/logs");
  expect(screen.getByText("What it prints.")).toBeTruthy();
});
