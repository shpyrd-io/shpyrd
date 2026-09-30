import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Sparkline, Stat } from "@shpyrd/ui/components/stat";
import { Stack } from "@shpyrd/ui/components/stack";
import { Section } from "../../section";
import { trend, trendOfErrors, trendOfRequests } from "../../samples";

export default function Page() {
  return (
    <>
      <Section title="A value, how it changed and how it went">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-3">
          <Card>
            <CardContent>
              <Stat
                label="Response time, 95th percentile"
                value="223"
                unit="ms"
                delta={{ value: "12%", direction: "down", good: true, against: "against yesterday" }}
                trend={trend}
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <Stat
                label="Requests"
                value="48.2"
                unit="a second"
                delta={{ value: "8%", direction: "up", against: "against yesterday" }}
                trend={trendOfRequests}
                tone="blue"
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <Stat
                label="Failed requests"
                value="1.4"
                unit="%"
                delta={{ value: "0.6", direction: "up", good: false, against: "against yesterday" }}
                trend={trendOfErrors}
                tone="error"
              />
            </CardContent>
          </Card>
        </div>
      </Section>
      <Section title="Only the value">
        <Card className="max-w-xs">
          <CardContent>
            <Stat label="Projects" value="12" />
          </CardContent>
        </Card>
      </Section>
      <Section title="Sparkline: by itself, in steps and as a line">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="spacious">
          <Sparkline values={trend} />
          <Sparkline values={trend} kind="line" tone="blue" />
          <Sparkline values={trendOfErrors} tone="error" className="w-40" />
        </Stack>
      </Section>
    </>
  );
}
