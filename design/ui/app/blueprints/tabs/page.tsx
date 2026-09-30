import { Boxes, History, ScrollText, Settings } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { Section } from "../../section";

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
          </TabsList>
          {panels}
        </Tabs>
      </Section>
      <Section title="Line">
        <Tabs defaultValue="overview">
          <TabsList variant="line">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="processes">Processes</TabsTrigger>
            <TabsTrigger value="releases">Releases</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
            <TabsTrigger value="settings" disabled>
              Settings
            </TabsTrigger>
          </TabsList>
          {panels}
        </Tabs>
      </Section>
      <Section title="Line, with counters">
        <Tabs defaultValue="processes">
          <TabsList variant="line">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="processes" counter={2}>
              Processes
            </TabsTrigger>
            <TabsTrigger value="releases" counter="11K">
              Releases
            </TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
          </TabsList>
          {panels}
        </Tabs>
      </Section>
      <Section title="Line, with icons">
        <Tabs defaultValue="releases">
          <TabsList variant="line">
            <TabsTrigger value="processes" icon={<Boxes />}>
              Processes
            </TabsTrigger>
            <TabsTrigger value="releases" icon={<History />} counter={12}>
              Releases
            </TabsTrigger>
            <TabsTrigger value="logs" icon={<ScrollText />}>
              Logs
            </TabsTrigger>
            <TabsTrigger value="settings" icon={<Settings />}>
              Settings
            </TabsTrigger>
          </TabsList>
          {panels}
        </Tabs>
      </Section>
      <Section title="Default, with icons and counters">
        <Tabs defaultValue="processes">
          <TabsList>
            <TabsTrigger value="processes" icon={<Boxes />} counter={2}>
              Processes
            </TabsTrigger>
            <TabsTrigger value="releases" icon={<History />} counter={12}>
              Releases
            </TabsTrigger>
          </TabsList>
          {panels}
        </Tabs>
      </Section>
    </>
  );
}

const panels = ["overview", "processes", "releases", "logs", "settings"].map((name) => (
  <TabsContent key={name} value={name} className="text-sm text-muted-foreground">
    The panel of {name}.
  </TabsContent>
));
