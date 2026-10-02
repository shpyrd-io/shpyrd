import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { PricingOption, PricingOptions } from "./pricing-options";

describe("PricingOptions", () => {
  it("needs only a plan's heading, and draws none of the parts it is not given", () => {
    const { container } = render(
      <PricingOptions>
        <PricingOption heading="Free" />
      </PricingOptions>,
    );
    expect(screen.getByRole("heading", { level: 3, name: "Free" })).toBeTruthy();
    expect(container.querySelector("[data-slot=pricing-option-price]")).toBeNull();
    expect(container.querySelector("[data-slot=pricing-option-features] ul")).toBeNull();
    expect(container.querySelector("[data-slot=pricing-option-usage] dl")).toBeNull();
  });

  it("shows the price with its currency, what it was, and what follows it", () => {
    render(<PricingOption heading="Team" price="16" originalPrice="20" trailingText="per seat" />);
    const price = screen.getByText("per seat").closest("[data-slot=pricing-option-price]");
    expect(price?.textContent).toContain("$16");
    expect(price?.textContent).toContain("Was $20");
  });

  it("marks what a plan leaves out, for a screen reader too", () => {
    const { container } = render(
      <PricingOption
        heading="Free"
        features={[{ children: "4 projects" }, { children: "Databases that stay awake", variant: "excluded" }]}
      />,
    );
    expect(screen.getByText("What's included")).toBeTruthy();
    const excluded = container.querySelector("li[data-variant=excluded]");
    expect(excluded?.textContent).toBe("Not included: Databases that stay awake");
    expect(container.querySelectorAll("li[data-variant=included]")).toHaveLength(1);
  });

  it("lists a plan's usage rates, each with how it is measured", () => {
    render(
      <PricingOption
        heading="Starter"
        usage={[
          { name: "Compute", note: "by actual use", value: "$0.02 / compute unit-hour" },
          { name: "Memory", value: "$0.005 / GiB-hour" },
        ]}
      />,
    );
    expect(screen.getByText("Usage")).toBeTruthy();
    expect(screen.getByText("Compute").closest("dt")?.textContent).toBe("Computeby actual use");
    expect(screen.getByText("$0.02 / compute unit-hour").tagName).toBe("DD");
    expect(screen.getByText("$0.005 / GiB-hour")).toBeTruthy();
  });

  it("shares rows between plans side by side, so their parts line up", () => {
    const { container } = render(
      <PricingOptions variant="cards" align="center">
        <PricingOption heading="Free" />
        <PricingOption heading="Starter" />
      </PricingOptions>,
    );
    const root = container.querySelector("[data-slot=pricing-options]");
    expect(root?.getAttribute("data-variant")).toBe("cards");
    expect(root?.getAttribute("data-align")).toBe("center");
    expect(root?.firstElementChild?.className).toContain("@2xl/pricing:grid-rows-[auto_auto_1fr_auto_auto]");
    for (const plan of container.querySelectorAll("[data-slot=pricing-option]")) {
      expect(plan.className).toContain("@2xl/pricing:grid-rows-subgrid");
      expect(plan.className).toContain("@2xl/pricing:row-span-5");
    }
  });

  it("shows the message only with the actions it is about", () => {
    const { rerender } = render(<PricingOption heading="Starter" message="Opened by hand." />);
    expect(screen.queryByText("Opened by hand.")).toBeNull();
    rerender(<PricingOption heading="Starter" actions={<button>Talk to us</button>} message="Opened by hand." />);
    expect(screen.getByText("Opened by hand.")).toBeTruthy();
  });
});
