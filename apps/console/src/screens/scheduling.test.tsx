import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SchedulingAlert } from "./overview";

describe("cluster scheduling warnings", () => {
  it("shows capacity failures and the required pool without metrics", () => {
    const html = renderToStaticMarkup(<SchedulingAlert scheduling={{ pending: [
      { namespace: "app-shop", name: "web-1", pool: "apps", capacity: true, message: "Insufficient cpu" },
    ] }} />);
    expect(html).toContain("Instances waiting for cluster resources");
    expect(html).toContain("app-shop/web-1");
    expect(html).toContain("apps pool");
    expect(html).toContain("Insufficient cpu");
    expect(html).toContain("Add or free capacity");
  });
  it("does not describe placement constraints as a resource shortage", () => {
    const html = renderToStaticMarkup(<SchedulingAlert scheduling={{ pending: [
      { namespace: "app-shop", name: "web-1", capacity: false, message: "untolerated taint" },
    ] }} />);
    expect(html).toContain("Instances waiting for scheduling");
    expect(html).toContain("untolerated taint");
    expect(html).not.toContain("resource shortage");
  });
  it("hides a cleared warning and distinguishes an unavailable check", () => {
    expect(renderToStaticMarkup(<SchedulingAlert scheduling={{ pending: [] }} />)).toBe("");
    expect(renderToStaticMarkup(<SchedulingAlert />)).toBe("");
    expect(renderToStaticMarkup(<SchedulingAlert scheduling={{ pending: [], error: "Could not check pending instances." }} />)).toContain("Scheduling status unavailable");
  });
});
