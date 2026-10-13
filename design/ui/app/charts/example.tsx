import { Card, CardContent } from "../../src/components/card";
import { MetricChart } from "../../src/components/metric-chart";
import type { ChartKind, ChartOptions } from "../../src/lib/metric-chart";
import { Section } from "../section";
import { chartSample } from "./samples";

export function ChartExample({kind, orientation}: {kind: ChartKind; orientation?: ChartOptions["orientation"]}) {
  const {data, options} = chartSample(kind);
  if (orientation) {
    options.orientation = orientation;
    data.title = `Funnel · ${orientation}`;
  }
  return <>
    <Section title="Full width"><Card><CardContent><MetricChart data={data} options={options} /></CardContent></Card></Section>
    <Section title="Inside a narrow card">
      <Card className="w-full max-w-sm"><CardContent><MetricChart data={data} options={options} /></CardContent></Card>
    </Section>
    <Section title="No observations"><Card><CardContent><MetricChart data={{...data, series: [], rows: []}} options={options} /></CardContent></Card></Section>
  </>;
}
