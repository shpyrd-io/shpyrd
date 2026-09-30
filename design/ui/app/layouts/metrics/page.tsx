import { Cpu, MemoryStick, Rocket } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { EventStrip } from "@shpyrd/ui/components/event-strip";
import { Meter } from "@shpyrd/ui/components/meter";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { Section } from "../../section";
import {
  events,
  FROM,
  memory,
  memoryReferences,
  NOW,
  releases,
  responseTime,
  throughput,
  trend,
  trendOfErrors,
  trendOfRequests,
} from "../../samples";

// A view made only of the library: what the metrics of a project could
// look like. The heading is an `h2`, as the page has its own.
export default function Page() {
  return (
    <Section title="The metrics of a project">
      <Card>
        <CardContent className="grid gap-6">
          <PageHeading
            as="h2"
            title="Hello World"
            iconEnd={
              <>
                <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                  hello-world
                </code>
                <StatusBadge type="success">Running</StatusBadge>
                <StatusBadge variant="secondary" type="success" qty="4/4">
                  web
                </StatusBadge>
                <StatusBadge variant="secondary" type="success" qty="1/1">
                  worker
                </StatusBadge>
              </>
            }
            actions={<Button icon={<Rocket />}>Deploy</Button>}
          />
          <Tabs defaultValue="metrics">
            <TabsList variant="line">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="metrics">Metrics</TabsTrigger>
              <TabsTrigger value="releases" counter={12}>
                Releases
              </TabsTrigger>
              <TabsTrigger value="logs">Logs</TabsTrigger>
            </TabsList>
            <TabsContent value="metrics" className="grid gap-6 pt-4">
              <div className="grid gap-x-8 gap-y-6 @3xl/page-layout:grid-cols-3">
                <Stat
                  label="Response time, 95th percentile"
                  value="223"
                  unit="ms"
                  delta={{ value: "12%", direction: "down", good: true, against: "against yesterday" }}
                  trend={trend}
                />
                <Stat
                  label="Requests"
                  value="48.2"
                  unit="a second"
                  delta={{ value: "8%", direction: "up", against: "against yesterday" }}
                  trend={trendOfRequests}
                  tone="blue"
                />
                <Stat
                  label="Failed requests"
                  value="1.4"
                  unit="%"
                  delta={{ value: "0.6", direction: "up", good: false, against: "against yesterday" }}
                  trend={trendOfErrors}
                  tone="error"
                />
              </div>
              <div className="grid gap-x-8 gap-y-6 border-t pt-6 @3xl/page-layout:grid-cols-2">
                <Meter
                  label="CPU"
                  unit="cores"
                  icon={<Cpu />}
                  used={1.25}
                  reserved={2}
                  capacity={3.5}
                />
                <Meter
                  label="Memory"
                  unit="GiB"
                  icon={<MemoryStick />}
                  used={1.7}
                  reserved={2}
                  capacity={2.5}
                />
              </div>
              <div className="grid gap-8 border-t pt-6">
                <EventStrip title="Events" rows={events} from={FROM} to={NOW} />
                <TimeChart
                  title="Response time"
                  description="How long the requests took"
                  unit="ms"
                  palette="shades"
                  series={responseTime}
                  markers={releases}
                />
                <TimeChart
                  title="Throughput"
                  description="Requests a second, by the class of the response"
                  unit="rps"
                  arrangement="stacked"
                  series={throughput}
                  markers={releases}
                />
                <TimeChart
                  title="Memory"
                  description="By process, against the memory of its size"
                  unit="bytes"
                  series={memory}
                  references={memoryReferences}
                />
              </div>
            </TabsContent>
            <TabsContent value="overview" className="pt-4 text-sm text-muted-foreground">
              The panel of overview.
            </TabsContent>
            <TabsContent value="releases" className="pt-4 text-sm text-muted-foreground">
              The panel of releases.
            </TabsContent>
            <TabsContent value="logs" className="pt-4 text-sm text-muted-foreground">
              The panel of logs.
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </Section>
  );
}
