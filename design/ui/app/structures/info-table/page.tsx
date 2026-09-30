import { Card, CardContent } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default: a grid of three columns">
        <InfoTable title="Summary">
          <InfoTableItem label="Name">Hello World</InfoTableItem>
          <InfoTableItem label="Release" mono>
            v12
          </InfoTableItem>
          <InfoTableItem label="Plan">Starter</InfoTableItem>
          <InfoTableItem label="Address" mono truncate>
            hello-world.platform.shpyrd.app
          </InfoTableItem>
          <InfoTableItem label="Region">São Paulo</InfoTableItem>
          <InfoTableItem label="Instances">
            2 <span className="text-muted-foreground">·</span> 512 MB each
          </InfoTableItem>
        </InfoTable>
      </Section>
      <Section title="In a card">
        <Card>
          <CardContent>
            <InfoTable title="Mail">
              <InfoTableItem label="Server" mono>
                smtp.shpyrd.app:587 <span className="text-muted-foreground">(STARTTLS)</span>
              </InfoTableItem>
              <InfoTableItem label="From" mono>
                no-reply@shpyrd.app
              </InfoTableItem>
              <InfoTableItem label="Status">
                <StatusBadge type="success">Running</StatusBadge>
              </InfoTableItem>
            </InfoTable>
          </CardContent>
        </Card>
      </Section>
      <Section title="Two columns, and an item that takes both">
        <InfoTable columns={2} className="max-w-xl">
          <InfoTableItem label="Owner">patrick@shpyrd.io</InfoTableItem>
          <InfoTableItem label="Made">29 September 2026</InfoTableItem>
          <InfoTableItem label="Description" span="full">
            A project is an application and everything it needs to run: its instances, its
            address and its releases.
          </InfoTableItem>
        </InfoTable>
      </Section>
      <Section title="Rows: the name at the start, the value at the end">
        <Card className="max-w-sm">
          <CardContent>
            <InfoTable title="Release" layout="rows">
              <InfoTableItem label="Version" mono>
                v12
              </InfoTableItem>
              <InfoTableItem label="Commit" mono>
                7f3c9a1e5b2d4c8f9a0b1c2d3e4f5a6b7c8d9e0f
              </InfoTableItem>
              <InfoTableItem label="Built in">48 seconds</InfoTableItem>
              <InfoTableItem label="By">Patrick</InfoTableItem>
            </InfoTable>
          </CardContent>
        </Card>
      </Section>
      <Section title="A value that is not there">
        <InfoTable columns={2} className="max-w-xl">
          <InfoTableItem label="Address" mono />
          <InfoTableItem label="Release">v1</InfoTableItem>
        </InfoTable>
      </Section>
      <Section title="With little room, fewer columns">
        <div className="max-w-xs rounded-xl p-4 ring-1 ring-foreground/10">
          <InfoTable title="Summary">
            <InfoTableItem label="Name">Hello World</InfoTableItem>
            <InfoTableItem label="Release" mono>
              v12
            </InfoTableItem>
            <InfoTableItem label="Plan">Starter</InfoTableItem>
          </InfoTable>
        </div>
      </Section>
    </>
  );
}
