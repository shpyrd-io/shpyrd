import { expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MetricChart } from "./metric-chart";
import { chartSample } from "../../app/charts/samples";

it("switches between a chart and exact values without exposing query metadata", () => {
  const {data, options} = chartSample("line");
  data.series[0].points[0].value = "9007199254740993.1234";
  data.series[0].points[1].value = null;
  render(<MetricChart data={data} options={options} />);
  expect(screen.queryByText("Query details")).toBeNull();
  fireEvent.click(screen.getByRole("button", {name: "Show the values as a table"}));
  expect(screen.getByRole("table")).toBeTruthy();
  expect(screen.getByText("9007199254740993.1234")).toBeTruthy();
  expect(screen.getByText("—")).toBeTruthy();
  expect(screen.queryByText("Observation history")).toBeNull();
  fireEvent.click(screen.getByRole("button", {name: "Show the chart"}));
  expect(screen.queryByRole("table")).toBeNull();
});

it("hides a series without modifying its source observations or table", () => {
  const {data, options} = chartSample("line");
  const before = JSON.stringify(data);
  render(<MetricChart data={data} options={options} />);
  const toggle = screen.getByRole("button", {name: "Toggle Direct"});
  fireEvent.click(toggle);
  expect(toggle.getAttribute("aria-pressed")).toBe("false");
  expect(JSON.stringify(data)).toBe(before);
  fireEvent.click(screen.getByRole("button", {name: "Show the values as a table"}));
  expect(screen.getByRole("columnheader", {name: "Direct"})).toBeTruthy();
});

it("uses keyboard navigation to inspect time series", () => {
  const {data, options} = chartSample("line");
  const {container} = render(<MetricChart data={data} options={options} />);
  const plot = screen.getByRole("img");
  fireEvent.keyDown(plot, {key: "Home"});
  expect(container.querySelector('.metric-legend-content strong')?.textContent).toBe("12");
  fireEvent.keyDown(plot, {key: "Escape"});
  expect(container.querySelector('.metric-legend-content strong')?.textContent).toBe("51");
});
