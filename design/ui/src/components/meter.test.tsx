import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Meter } from "./meter";

const widths = () =>
  [...screen.getByRole("meter").children].map((part) => (part as HTMLElement).style.width);

describe("Meter", () => {
  it("says how much is used, of how much there is", () => {
    render(<Meter label="CPU" used={12.5} reserved={20} capacity={35} />);
    const meter = screen.getByRole("meter", { name: "CPU: 12.5 of 35 used" });
    expect(meter.getAttribute("aria-valuenow")).toBe("12.5");
    expect(meter.getAttribute("aria-valuemax")).toBe("35");
  });

  it("writes the percentage, its part after the point smaller", () => {
    const { container } = render(<Meter label="CPU" used={12.5} capacity={35} />);
    expect(container.querySelector(".font-heading")?.textContent).toBe("35.7%");
  });

  it("draws what is used, what is reserved and not used, and what is free", () => {
    render(<Meter label="Volumes" used={100} reserved={250} capacity={500} />);
    expect(widths()).toEqual(["20%", "30%", "50%"]);
    expect([...screen.getByRole("meter").children].map((p) => (p as HTMLElement).title)).toEqual([
      "Used: 100",
      "Reserved: 250",
      "Free: 250",
    ]);
  });

  it("draws nothing of what is reserved when more than that is used", () => {
    render(<Meter label="CPU" used={300} reserved={250} capacity={500} />);
    expect(widths()).toEqual(["60%", "40%"]);
  });

  it("stops at the capacity when more than there is is used", () => {
    render(<Meter label="CPU" used={600} capacity={500} />);
    expect(widths()).toEqual(["100%"]);
  });

  it("takes the ink of a warning, then of an error, as it fills", () => {
    const tone = (used: number) => {
      const { container, unmount } = render(<Meter label="Disk" used={used} capacity={100} />);
      const ink = container.firstElementChild?.getAttribute("data-tone");
      unmount();
      return ink;
    };
    expect([50, 75, 89, 90, 100].map(tone)).toEqual([
      "orange",
      "warning",
      "warning",
      "error",
      "error",
    ]);
  });

  it("keeps the ink it is given, however full", () => {
    const { container } = render(<Meter label="Disk" used={99} capacity={100} tone="blue" />);
    expect(container.firstElementChild?.getAttribute("data-tone")).toBe("blue");
  });

  it("writes the three values under the drawing, so none is only in it", () => {
    const { container } = render(<Meter label="CPU" used={12.5} reserved={20} capacity={35} />);
    expect([...container.querySelectorAll("dt")].map((t) => t.textContent)).toEqual([
      "Used",
      "Reserved",
      "Capacity",
    ]);
    expect([...container.querySelectorAll("dd")].map((d) => d.textContent)).toEqual([
      "12.5",
      "20",
      "35",
    ]);
  });
});
