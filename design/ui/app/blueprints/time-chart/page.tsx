import { Card, CardContent } from "@shpyrd/ui/components/card";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { Section } from "../../section";
import {
  cpu,
  instances,
  memory,
  memoryReferences,
  releases,
  responseTime,
  throughput,
} from "../../samples";

export default function Page() {
  return (
    <>
      <Section title="Shades of one ink: steps of the same thing">
        <Card>
          <CardContent>
            <TimeChart
              title="Response time"
              description="How long the requests took, in the last hour"
              unit="ms"
              palette="shades"
              series={responseTime}
              markers={releases}
            />
          </CardContent>
        </Card>
      </Section>
      <Section title="Stacked: parts of a whole, each with what it means">
        <Card>
          <CardContent>
            <TimeChart
              title="Throughput"
              description="Requests a second, by the class of the response"
              unit="rps"
              arrangement="stacked"
              series={throughput}
              markers={releases}
            />
          </CardContent>
        </Card>
      </Section>
      <Section title="Against what was allocated">
        <Card>
          <CardContent>
            <TimeChart
              title="Memory"
              description="By process, against the memory of its size"
              unit="bytes"
              series={memory}
              references={memoryReferences}
            />
          </CardContent>
        </Card>
      </Section>
      <Section title="Lines">
        <Card>
          <CardContent>
            <TimeChart
              title="CPU"
              description="By process, of what its size gives it"
              unit="%"
              kind="line"
              series={cpu}
            />
          </CardContent>
        </Card>
      </Section>
      <Section title="A count">
        <Card>
          <CardContent>
            <TimeChart
              title="Instances"
              description="Running, by process"
              unit="count"
              series={instances}
            />
          </CardContent>
        </Card>
      </Section>
      <Section title="Nothing to show">
        <Card>
          <CardContent>
            <TimeChart title="Network" description="What came in and what went out" series={[]} />
          </CardContent>
        </Card>
      </Section>
    </>
  );
}
