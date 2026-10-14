import { Cpu, HardDrive, MemoryStick } from "lucide-react";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Meter } from "@shpyrd/ui/components/meter";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Used, reserved and capacity">
        <div className="grid gap-4 @3xl/page-layout:grid-cols-3">
          <Card>
            <CardContent>
              <Meter
                label="CPU"
                unit="cores"
                icon={<Cpu />}
                used={12.5}
                reserved={20}
                capacity={35}
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <Meter
                label="Memory"
                unit="GiB"
                icon={<MemoryStick />}
                used={49}
                reserved={56}
                capacity={64}
              />
            </CardContent>
          </Card>
          <Card>
            <CardContent>
              <Meter
                label="Disk"
                unit="GiB"
                icon={<HardDrive />}
                used={186}
                reserved={192}
                capacity={200}
              />
            </CardContent>
          </Card>
        </div>
      </Section>
      <Section title="Without what is reserved">
        <Card className="max-w-sm">
          <CardContent>
            <Meter label="Instances" used={38} capacity={110} />
          </CardContent>
        </Card>
      </Section>
      <Section title="An ink of its own">
        <Card className="max-w-sm">
          <CardContent>
            <Meter label="Volumes" unit="GiB" tone="blue" used={120} reserved={260} capacity={500} />
          </CardContent>
        </Card>
      </Section>
    </>
  );
}
