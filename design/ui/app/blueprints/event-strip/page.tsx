import { Card, CardContent } from "@shpyrd/ui/components/card";
import { EventStrip } from "@shpyrd/ui/components/event-strip";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { Section } from "../../section";
import { events, FROM, NOW, releases, responseTime } from "../../samples";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Card>
          <CardContent>
            <EventStrip title="Events" rows={events} from={FROM} to={NOW} />
          </CardContent>
        </Card>
      </Section>
      <Section title="Over a chart, sharing the time">
        <Card>
          <CardContent className="grid gap-6">
            <EventStrip title="Events" rows={events} from={FROM} to={NOW} />
            <TimeChart
              title="Response time"
              unit="ms"
              palette="shades"
              series={responseTime}
              markers={releases}
            />
          </CardContent>
        </Card>
      </Section>
    </>
  );
}
